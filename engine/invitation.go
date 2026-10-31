package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"p2p-park/internal/netx"
)

const (
	InvitationVersion       = 1
	MaxInvitationSize       = 2048 // encoded URI, including prefix
	MaxInvitationBootstraps = 4
	invitationPrefix        = "p2p://invite/"
	maxInvitationJSON       = 1400
)

var (
	ErrInvalidInvitation            = errors.New("engine: invalid invitation")
	ErrUnsupportedInvitationVersion = errors.New("engine: unsupported invitation version")
	ErrInvitationExpired            = errors.New("engine: invitation expired")
	ErrBootstrapUnavailable         = errors.New("engine: no authenticated bootstrap available")
)

// BootstrapPeer pins both public identities at a canonical numeric TCP endpoint.
// It is discovery information, not a certificate or membership approval.
type BootstrapPeer struct {
	Address   string `json:"address"`
	PeerID    string `json:"peer_id"`
	SigningID string `json:"signing_id"`
}

// Invitation contains only public discovery data. Obtain it from a trusted source:
// v1 is not issuer-signed and changing an entire invitation cannot be detected
// without a separately trusted reference. Never treat possession as authorization.
type Invitation struct {
	Version     int             `json:"version"`
	NetworkID   string          `json:"network_id"`
	Bootstraps  []BootstrapPeer `json:"bootstraps"`
	ExpiresUnix int64           `json:"expires_unix,omitempty"`
}

// JoinOptions is local operator policy, never imported from invitation data.
// Public destinations are allowed by default. Private/loopback/CGNAT endpoints
// additionally require a matching prefix. Prohibited endpoints cannot be enabled.
// This policy does not broaden automatic discovery scopes or membership.
type JoinOptions struct{ AllowedCIDRs []string }

// JoinResult records completed authenticated attempts, not continued liveness.
// Partial successes remain connected even if another attempt fails or is cancelled.
type JoinResult struct {
	Connected []BootstrapPeer
	Failures  []error
}

func invitationError(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInvitation, message)
}
func invitationID(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

// Conservative special-use exclusions: v1 deliberately favors predictable direct
// TCP endpoints over DNS, transition mechanisms and special-purpose IP services.
var prohibitedInvitationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}
var publicIPv6 = netip.MustParsePrefix("2000::/3")
var invitationCGNAT = netip.MustParsePrefix("100.64.0.0/10")

func invitationEndpoint(address string) (netip.Addr, error) {
	if len(address) > 64 {
		return netip.Addr{}, invitationError("endpoint too long")
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil || ap.Port() == 0 || ap.String() != address {
		return netip.Addr{}, invitationError("endpoint must be canonical numeric IP:port")
	}
	ip := ap.Addr()
	if ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || (!ip.IsGlobalUnicast() && !ip.IsLoopback()) {
		return netip.Addr{}, invitationError("prohibited endpoint")
	}
	if ip.Is6() && !ip.IsPrivate() && !ip.IsLoopback() && !publicIPv6.Contains(ip) {
		return netip.Addr{}, invitationError("unsupported IPv6 scope")
	}
	for _, prefix := range prohibitedInvitationPrefixes {
		if prefix.Contains(ip) {
			return netip.Addr{}, invitationError("prohibited endpoint")
		}
	}
	return ip, nil
}

// canonical validates and copies without mutating caller slices. Sorting by
// address makes the single v1 representation independent of input peer order.
func (i Invitation) canonical(now time.Time) ([]byte, Invitation, error) {
	if i.Version != InvitationVersion {
		return nil, Invitation{}, ErrUnsupportedInvitationVersion
	}
	if !invitationID(i.NetworkID) {
		return nil, Invitation{}, invitationError("invalid network ID")
	}
	if len(i.Bootstraps) < 1 || len(i.Bootstraps) > MaxInvitationBootstraps {
		return nil, Invitation{}, invitationError("bootstrap count outside 1..4")
	}
	if i.ExpiresUnix < 0 || i.ExpiresUnix > 253402300799 {
		return nil, Invitation{}, invitationError("invalid expiration")
	}
	if i.ExpiresUnix != 0 && i.ExpiresUnix <= now.Unix() {
		return nil, Invitation{}, ErrInvitationExpired
	}
	i.Bootstraps = append([]BootstrapPeer(nil), i.Bootstraps...)
	sort.Slice(i.Bootstraps, func(a, b int) bool { return i.Bootstraps[a].Address < i.Bootstraps[b].Address })
	for index, p := range i.Bootstraps {
		if _, err := invitationEndpoint(p.Address); err != nil {
			return nil, Invitation{}, err
		}
		if !invitationID(p.PeerID) || !invitationID(p.SigningID) {
			return nil, Invitation{}, invitationError("invalid bootstrap identity")
		}
		if index > 0 && i.Bootstraps[index-1].Address == p.Address {
			return nil, Invitation{}, invitationError("duplicate endpoint")
		}
	}
	b, err := json.Marshal(i)
	if err != nil || len(b) > maxInvitationJSON {
		return nil, Invitation{}, invitationError("payload too large")
	}
	return b, i, nil
}

// Encode returns p2p://invite/<unpadded-base64url-canonical-JSON>.
// It performs no I/O. Unknown extensions require a new invitation version.
func (i Invitation) Encode() (string, error) {
	b, _, err := i.canonical(time.Now())
	if err != nil {
		return "", err
	}
	uri := invitationPrefix + base64.RawURLEncoding.EncodeToString(b)
	if len(uri) > MaxInvitationSize {
		return "", invitationError("URI too large")
	}
	return uri, nil
}

// ParseInvitation accepts only the exact v1 canonical representation. Whitespace,
// duplicate/unknown fields, alternate encodings, trailing values and URL wrappers
// are rejected. Size is bounded before decoding; parsing performs no network I/O.
func ParseInvitation(uri string) (Invitation, error) {
	if len(uri) > MaxInvitationSize || !strings.HasPrefix(uri, invitationPrefix) {
		return Invitation{}, invitationError("invalid URI or size")
	}
	payload := strings.TrimPrefix(uri, invitationPrefix)
	b, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil || len(b) > maxInvitationJSON || base64.RawURLEncoding.EncodeToString(b) != payload {
		return Invitation{}, invitationError("invalid payload encoding")
	}
	var i Invitation
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&i) != nil {
		return Invitation{}, invitationError("invalid JSON fields")
	}
	canonical, owned, err := i.canonical(time.Now())
	if err != nil {
		return Invitation{}, err
	}
	if !bytes.Equal(canonical, b) {
		return Invitation{}, invitationError("noncanonical JSON")
	}
	return owned, nil
}

// Invitation advertises this node's own key pair only, never automatically
// endorsing peer referrals. Empty addresses uses Address(); wildcard listeners
// require explicit reachable numeric addresses. No reachability/NAT probe occurs.
func (e *Engine) Invitation(addresses []string, expires time.Time) (Invitation, error) {
	if err := e.running(); err != nil {
		return Invitation{}, err
	}
	if len(addresses) == 0 {
		addresses = []string{e.Address()}
	}
	if len(addresses) > MaxInvitationBootstraps {
		return Invitation{}, invitationError("too many advertised endpoints")
	}
	i := Invitation{Version: InvitationVersion, NetworkID: e.networkID}
	if !expires.IsZero() {
		i.ExpiresUnix = expires.Unix()
	}
	for _, addr := range addresses {
		i.Bootstraps = append(i.Bootstraps, BootstrapPeer{Address: addr, PeerID: e.PeerID(), SigningID: e.SigningID()})
	}
	_, owned, err := i.canonical(time.Now())
	return owned, err
}

func invitationPolicy(options JoinOptions) ([]netip.Prefix, error) {
	if len(options.AllowedCIDRs) > 16 {
		return nil, invitationError("too many local policy prefixes")
	}
	var prefixes []netip.Prefix
	for _, s := range options.AllowedCIDRs {
		p, err := netip.ParsePrefix(s)
		if err != nil || p.Addr().Is4In6() || p.Masked() != p {
			return nil, invitationError("invalid local policy prefix")
		}
		prefixes = append(prefixes, p)
	}
	return prefixes, nil
}
func invitationScope(ip netip.Addr, prefixes []netip.Prefix) bool {
	if !ip.IsPrivate() && !ip.IsLoopback() && !invitationCGNAT.Contains(ip) {
		return true
	}
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Join validates the complete invitation and local scope policy before any dial.
// The engine must already run in the invitation's overlay. Every bootstrap is
// attempted sequentially with a five-second cap; successes belong to Start's
// lifetime. Nil error means at least one success; inspect Failures for partial
// results. No trust configuration, admission or discovery scopes are changed.
func (e *Engine) Join(ctx context.Context, i Invitation, options JoinOptions) (JoinResult, error) {
	var result JoinResult
	if ctx == nil {
		return result, errors.New("engine: nil context")
	}
	finish, err := e.beginOperation(ctx)
	if err != nil {
		return result, err
	}
	defer finish()
	_, i, err = i.canonical(time.Now())
	if err != nil {
		return result, err
	}
	if i.NetworkID != e.networkID {
		return result, invitationError("network ID differs from engine")
	}
	prefixes, err := invitationPolicy(options)
	if err != nil {
		return result, err
	}
	for _, p := range i.Bootstraps {
		ip, _ := invitationEndpoint(p.Address) // canonical has validated every endpoint
		if !invitationScope(ip, prefixes) {
			return result, invitationError("endpoint outside local invitation scope")
		}
		if pin := e.peerPins[netx.Addr(p.Address)]; pin != "" && pin != p.PeerID {
			return result, invitationError("bootstrap conflicts with configured pin")
		}
	}
	joinCtx := ctx
	if i.ExpiresUnix != 0 {
		var cancel context.CancelFunc
		joinCtx, cancel = context.WithDeadline(ctx, time.Unix(i.ExpiresUnix, 0))
		defer cancel()
	}
	for _, p := range i.Bootstraps {
		if err := joinCtx.Err(); err != nil {
			return result, invitationJoinContextError(ctx, i, err)
		}
		attempt, cancel := context.WithTimeout(joinCtx, 5*time.Second)
		err := e.node.ConnectPinnedContext(attempt, netx.Addr(p.Address), p.PeerID, p.SigningID)
		cancel()
		if err == nil {
			result.Connected = append(result.Connected, p)
		} else {
			result.Failures = append(result.Failures, fmt.Errorf("bootstrap %s: %w", p.Address, err))
		}
		if err := joinCtx.Err(); err != nil {
			return result, invitationJoinContextError(ctx, i, err)
		}
	}
	if len(result.Connected) == 0 {
		return result, errors.Join(append([]error{ErrBootstrapUnavailable}, result.Failures...)...)
	}
	return result, nil
}
func invitationJoinContextError(parent context.Context, i Invitation, fallback error) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if i.ExpiresUnix != 0 && time.Now().Unix() >= i.ExpiresUnix {
		return ErrInvitationExpired
	}
	return fallback
}
