#!/usr/bin/env python3
"""Check the production dependency closure of the public engine and example."""
import subprocess

for package in ("./engine", "./examples/echo", "./examples/netcheck"):
    deps = subprocess.check_output(["go", "list", "-deps", package], text=True).splitlines()
    forbidden = ("p2p-park/internal/app/", "p2p-park/internal/storage/",
                 "p2p-park/internal/park-node", "p2p-park/internal/proto",
                 "go.etcd.io/bbolt", "p2p-park/internal/gameproto")
    violations = [dep for dep in deps if any(dep.startswith(p) for p in forbidden)]
    if violations:
        raise SystemExit(f"{package} depends on application packages: {violations}")
    print(f"PASS {package}: no game/schema/database dependencies")

# Stop traversal at the public boundary: its implementation necessarily contains
# networking internals. Other application dependencies must never reach them.
import json
roots = ["p2p-park/internal/park-node", "p2p-park/cmd/park-node"]
raw = subprocess.check_output(["go", "list", "-deps", "-json", "./cmd/park-node"], text=True)
decoder = json.JSONDecoder()
packages = {}
while raw.strip():
    obj, consumed = decoder.raw_decode(raw.lstrip())
    packages[obj["ImportPath"]] = obj.get("Imports", [])
    raw = raw.lstrip()[consumed:]
forbidden = ("p2p-park/internal/p2p", "p2p-park/internal/netx",
             "p2p-park/internal/dht", "p2p-park/internal/discovery",
             "p2p-park/internal/wire", "p2p-park/internal/proto",
             "p2p-park/internal/crypto/noiseconn")
seen = set()
def visit(package):
    if package == "p2p-park/engine" or package in seen:
        return
    seen.add(package)
    if package in forbidden:
        raise SystemExit(f"Park bypasses the public engine boundary: {package}")
    for dependency in packages.get(package, []):
        visit(dependency)
for root in roots:
    visit(root)
print("PASS Park/CLI/application dependencies reach networking only through engine")
