#!/usr/bin/env python3
"""Check the production dependency closure of the public engine and example."""
import subprocess

for package in ("./engine", "./examples/echo", "./examples/netcheck"):
    deps = subprocess.check_output(["go", "list", "-deps", package], text=True).splitlines()
    forbidden = ("github.com/TylerPetri/p2p-networking-project/internal/app/", "github.com/TylerPetri/p2p-networking-project/internal/storage/",
                 "github.com/TylerPetri/p2p-networking-project/internal/park-node", "github.com/TylerPetri/p2p-networking-project/internal/proto",
                 "go.etcd.io/bbolt", "github.com/TylerPetri/p2p-networking-project/internal/gameproto")
    violations = [dep for dep in deps if any(dep.startswith(p) for p in forbidden)]
    if violations:
        raise SystemExit(f"{package} depends on application packages: {violations}")
    print(f"PASS {package}: no game/schema/database dependencies")

# Stop traversal at the public boundary: its implementation necessarily contains
# networking internals. Other application dependencies must never reach them.
import json
roots = ["github.com/TylerPetri/p2p-networking-project/internal/park-node", "github.com/TylerPetri/p2p-networking-project/cmd/park-node"]
raw = subprocess.check_output(["go", "list", "-deps", "-json", "./cmd/park-node"], text=True)
decoder = json.JSONDecoder()
packages = {}
while raw.strip():
    obj, consumed = decoder.raw_decode(raw.lstrip())
    packages[obj["ImportPath"]] = obj.get("Imports", [])
    raw = raw.lstrip()[consumed:]
forbidden = ("github.com/TylerPetri/p2p-networking-project/internal/p2p", "github.com/TylerPetri/p2p-networking-project/internal/netx",
             "github.com/TylerPetri/p2p-networking-project/internal/dht", "github.com/TylerPetri/p2p-networking-project/internal/discovery",
             "github.com/TylerPetri/p2p-networking-project/internal/wire", "github.com/TylerPetri/p2p-networking-project/internal/proto",
             "github.com/TylerPetri/p2p-networking-project/internal/crypto/noiseconn")
seen = set()
def visit(package):
    if package == "github.com/TylerPetri/p2p-networking-project/engine" or package in seen:
        return
    seen.add(package)
    if package in forbidden:
        raise SystemExit(f"Park bypasses the public engine boundary: {package}")
    for dependency in packages.get(package, []):
        visit(dependency)
for root in roots:
    visit(root)
print("PASS Park/CLI/application dependencies reach networking only through engine")
