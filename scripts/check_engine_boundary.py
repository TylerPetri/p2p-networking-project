#!/usr/bin/env python3
"""Check the production dependency closure of the public engine and example."""
import subprocess

for package in ("./engine", "./examples/echo"):
    deps = subprocess.check_output(["go", "list", "-deps", package], text=True).splitlines()
    forbidden = ("p2p-park/internal/app/", "p2p-park/internal/storage/",
                 "p2p-park/internal/park-node", "p2p-park/internal/proto",
                 "go.etcd.io/bbolt")
    violations = [dep for dep in deps if any(dep.startswith(p) for p in forbidden)]
    if violations:
        raise SystemExit(f"{package} depends on application packages: {violations}")
    print(f"PASS {package}: no game/schema/database dependencies")
