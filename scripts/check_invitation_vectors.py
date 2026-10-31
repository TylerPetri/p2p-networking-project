#!/usr/bin/env python3
"""Independent Python/std-lib reader for the committed Go v1 wire fixtures.
No networking, private keys or cryptographic implementation. Normative: INVITATIONS.md.
"""
import base64
import ipaddress
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "p2p://invite/"
BLOCKED = [ipaddress.ip_network(s) for s in (
    "0.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24",
    "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
    "224.0.0.0/4", "240.0.0.0/4", "168.63.129.16/32", "100.100.100.200/32",
    "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20")]
HEX = re.compile(r"[0-9a-f]{64}\Z")


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def endpoint(value):
    if not isinstance(value, str) or len(value) > 64 or "%" in value:
        raise ValueError("address")
    if value.startswith("["):
        host, port = value[1:].split("]:")
        ip = ipaddress.IPv6Address(host)
        canonical = f"[{ip.compressed}]:{int(port)}"
        if ip.ipv4_mapped:
            raise ValueError("mapped")
        local = ip in ipaddress.ip_network("fc00::/7") or ip.is_loopback
        if not local and ip not in ipaddress.ip_network("2000::/3"):
            raise ValueError("IPv6 scope")
    else:
        host, port = value.split(":")
        ip = ipaddress.IPv4Address(host)
        canonical = f"{ip}:{int(port)}"
    if not 1 <= int(port) <= 65535 or canonical != value:
        raise ValueError("noncanonical endpoint")
    if ip.is_unspecified or ip.is_multicast or ip.is_link_local or any(ip in n for n in BLOCKED if ip.version == n.version):
        raise ValueError("prohibited")


def encode(inv):
    raw = json.dumps(inv, ensure_ascii=True, separators=(",", ":"))
    return raw, PREFIX + base64.urlsafe_b64encode(raw.encode()).decode().rstrip("=")


def parse(uri, now):
    try:
        if len(uri.encode()) > 2048 or not uri.startswith(PREFIX):
            raise ValueError("URI")
        payload = uri[len(PREFIX):]
        if not re.fullmatch(r"[A-Za-z0-9_-]+", payload):
            raise ValueError("base64url")
        data = base64.b64decode(payload + "=" * (-len(payload) % 4), altchars=b"-_", validate=True)
        if len(data) > 1400 or base64.urlsafe_b64encode(data).decode().rstrip("=") != payload:
            raise ValueError("size/encoding")
        obj = json.loads(data.decode("utf-8"), object_pairs_hook=pairs)
        if not isinstance(obj, dict) or set(obj) - {"version", "network_id", "bootstraps", "expires_unix"}:
            raise ValueError("fields")
        if type(obj.get("version")) is not int:
            raise ValueError("version type")
        if obj["version"] != 1:
            return None, "unsupported"
        if not isinstance(obj.get("network_id"), str) or not HEX.fullmatch(obj["network_id"]):
            raise ValueError("network")
        peers = obj.get("bootstraps")
        if not isinstance(peers, list) or not 1 <= len(peers) <= 4:
            raise ValueError("peers")
        expiry = obj.get("expires_unix", 0)
        if type(expiry) is not int or not 0 <= expiry <= 253402300799:
            raise ValueError("expiry")
        if expiry and expiry <= now:
            return None, "expired"
        canonical_peers = []
        for peer in peers:
            if not isinstance(peer, dict) or set(peer) != {"address", "peer_id", "signing_id"}:
                raise ValueError("peer fields")
            endpoint(peer["address"])
            if any(not isinstance(peer[k], str) or not HEX.fullmatch(peer[k]) for k in ("peer_id", "signing_id")):
                raise ValueError("identity")
            canonical_peers.append({k: peer[k] for k in ("address", "peer_id", "signing_id")})
        canonical_peers.sort(key=lambda p: p["address"])
        if len({p["address"] for p in canonical_peers}) != len(peers):
            raise ValueError("duplicate endpoint")
        inv = dict(version=1, network_id=obj["network_id"], bootstraps=canonical_peers)
        if expiry:
            inv["expires_unix"] = expiry
        raw, encoded = encode(inv)
        if encoded != uri or raw.encode() != data:
            raise ValueError("noncanonical")
        return inv, ""
    except (ValueError, TypeError, KeyError, UnicodeError):
        return None, "invalid"


def main():
    corpus = json.loads((ROOT / "engine/testdata/invitations/v1.json").read_text())
    for case in corpus["cases"]:
        inv, error = parse(case["uri"], corpus["reference_unix"])
        if error != case["parse_error"]:
            raise SystemExit(f"FAIL {case['name']}: {error!r}, expected {case['parse_error']!r}")
        if inv:
            raw, uri = encode(inv)
            if raw != case["json"] or uri != case["uri"]:
                raise SystemExit(f"FAIL canonical bytes: {case['name']}")
    print(f"PASS independent Python codec: {len(corpus['cases'])} fixed-clock public invitation vectors (no network I/O)")


if __name__ == "__main__":
    main()
