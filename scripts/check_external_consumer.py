#!/usr/bin/env python3
"""Build/run a temporary, independent Go consumer. No published or nested module."""
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SOURCE = r'''package main
import("context";"encoding/hex";"errors";"time";"p2p-park/engine")
func main(){
 ctx,stop:=context.WithTimeout(context.Background(),5*time.Second);defer stop()
 id,err:=engine.NewNetworkID();if err!=nil{panic(err)}
 a,err:=engine.New(engine.Config{NetworkID:id,Application:"external-consumer"});if err!=nil{panic(err)};defer a.Close()
 if err=a.Start(ctx);err!=nil{panic(err)}
 inv,err:=a.Invitation(nil,time.Time{});if err!=nil{panic(err)}
 text,err:=inv.Encode();if err!=nil{panic(err)}
 parsed,err:=engine.ParseInvitation(text);if err!=nil{panic(err)}
 b,err:=engine.New(engine.Config{NetworkID:parsed.NetworkID,Application:"external-consumer"});if err!=nil{panic(err)};defer b.Close()
 if err=b.Start(ctx);err!=nil{panic(err)}
 if _,err=b.Join(ctx,parsed,engine.JoinOptions{});!errors.Is(err,engine.ErrInvalidInvitation){panic("local scope not enforced")}
 result,err:=b.Join(ctx,parsed,engine.JoinOptions{AllowedCIDRs:[]string{"127.0.0.1/32"}});if err!=nil||len(result.Connected)!=1{panic("join failed")}
 if err=b.Send(ctx,a.PeerID(),"test",[]byte("independent"));err!=nil{panic(err)}
 select{case m:=<-a.Messages():if string(m.Data)!="independent"||hex.EncodeToString(m.SigningKey)!=b.SigningID(){panic("origin mismatch")};case <-ctx.Done():panic(ctx.Err())}
 if err=b.Close();err!=nil{panic(err)};if err=b.Close();err!=nil{panic(err)}
 if err=b.Start(ctx);!errors.Is(err,engine.ErrClosed){panic("closed instance restarted")}
}
'''

with tempfile.TemporaryDirectory(prefix="p2p-external-consumer-") as temp:
    p = Path(temp)
    (p / "go.mod").write_text("module example/engine-consumer\n\ngo 1.24.1\n\nrequire p2p-park v0.0.0\nreplace p2p-park => " + json.dumps(str(ROOT)) + "\n")
    (p / "main.go").write_text(SOURCE)
    subprocess.run(["go", "run", "-race", "-mod=mod", "."], cwd=p, check=True)
print("PASS external Go consumer: public imports, invitation policy, authenticated message and lifecycle; no module publication")
