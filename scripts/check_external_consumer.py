#!/usr/bin/env python3
"""Fresh public SDK consumer. Default: local replace. --version REV: remote, no replace.

Neither mode tags or deploys anything; engine peers use loopback only.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/TylerPetri/p2p-networking-project"
SOURCE = r'''package main
import (
 "context"
 "encoding/hex"
 "errors"
 "fmt"
 "net"
 "time"
 "github.com/TylerPetri/p2p-networking-project/engine"
)
func check(err error) { if err != nil { panic(err) } }
func main() {
 ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
 defer stop()
 id, err := engine.NewNetworkID(); check(err)
 a, err := engine.New(engine.Config{NetworkID:id, Application:"external-consumer"}); check(err)
 defer a.Close()
 check(a.Start(ctx))
 inv, err := a.Invitation(nil,time.Time{}); check(err)
 text, err := inv.Encode(); check(err)
 parsed, err := engine.ParseInvitation(text); check(err)
 if parsed.Bootstraps[0].PeerID != a.PeerID() || parsed.Bootstraps[0].SigningID != a.SigningID() {
  panic("invitation identity mismatch")
 }
 b, err := engine.New(engine.Config{NetworkID:parsed.NetworkID, Application:"external-consumer"}); check(err)
 defer b.Close()
 check(b.Start(ctx))
 if _,err = b.Join(ctx,parsed,engine.JoinOptions{}); !errors.Is(err,engine.ErrInvalidInvitation) {
  panic("local scope not enforced")
 }
 opts := engine.JoinOptions{AllowedCIDRs:[]string{"127.0.0.1/32"}}
 bad := parsed
 bad.Bootstraps = append([]engine.BootstrapPeer(nil), parsed.Bootstraps...)
 bad.Bootstraps[0].SigningID = b.SigningID()
 if _,err = b.Join(ctx,bad,opts); !errors.Is(err,engine.ErrBootstrapUnavailable) {
  panic("substituted signing pin accepted")
 }
 result,err := b.Join(ctx,parsed,opts); check(err)
 if len(result.Connected) != 1 { panic("join incomplete") }
 check(b.Send(ctx,a.PeerID(),"test",[]byte("independent")))
 select {
 case m := <-a.Messages():
  if m.Topic != "test" || string(m.Data) != "independent" || hex.EncodeToString(m.SigningKey) != b.SigningID() {
   panic("authenticated delivery mismatch")
  }
 case <-ctx.Done(): panic(ctx.Err())
 }
 addresses := []string{a.Address(), b.Address()}
 check(b.Close())
 check(b.Close())
 if err = b.Start(ctx); !errors.Is(err,engine.ErrClosed) { panic("closed instance restarted") }
 check(a.Close())
 for _,addr := range addresses {
  listener,err := net.Listen("tcp",addr); check(err); check(listener.Close())
 }
 fmt.Println("PASS pinned invitation, substitution denial, authenticated delivery and clean shutdown")
}
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", help="remote commit, pseudo-version or tag; no local replace")
    args = parser.parse_args()
    minimum = re.search(r"(?m)^go (\S+)$", (ROOT / "go.mod").read_text()).group(1)
    with tempfile.TemporaryDirectory(prefix="p2p-external-consumer-") as temp:
        p = Path(temp)
        env = dict(os.environ, GOWORK="off", GOTOOLCHAIN="local")
        mod = f"module example/engine-consumer\n\ngo {minimum}\n"
        if args.version:
            env["GOMODCACHE"] = str(p / "module-cache")
        else:
            mod += f"\nrequire {MODULE} v0.0.0\nreplace {MODULE} => {json.dumps(str(ROOT))}\n"
        (p / "go.mod").write_text(mod)
        (p / "main.go").write_text(SOURCE)
        if args.version:
            subprocess.run(["go", "get", f"{MODULE}@{args.version}"], cwd=p, env=env, check=True)
        subprocess.run(["go", "mod", "tidy"], cwd=p, env=env, check=True)
        metadata = json.loads(subprocess.check_output(
            ["go", "list", "-m", "-json", MODULE], cwd=p, env=env, text=True))
        if args.version and ("Replace" in metadata or "replace " in (p / "go.mod").read_text()):
            raise SystemExit("remote verification must not use replace")
        subprocess.run(["go", "run", "-race", "."], cwd=p, env=env, check=True)
        if args.version:
            env["GOBIN"] = str(p / "bin")
            subprocess.run(["go", "install", f"{MODULE}/cmd/park-node@{metadata['Version']}"],
                           cwd=p, env=env, check=True)
            subprocess.run([str(p / "bin" / "park-node"), "-h"],
                           cwd=p, env=env, check=True, stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL)
        print(json.dumps({"mode": "remote" if args.version else "local-replace",
                          "query": args.version, "module": metadata["Path"],
                          "version": metadata["Version"], "sum": metadata.get("Sum"),
                          "replace": "Replace" in metadata}, sort_keys=True), flush=True)


if __name__ == "__main__":
    main()
