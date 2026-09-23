#!/usr/bin/env python3
"""Offline process/argv/TTY/signal regression, using one unchanged host binary."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
HELPER = r'''
package main
import("encoding/json";"fmt";"io";"os";"time")
func main(){
 args:=os.Args[1:]
 if len(args)>0 && args[0]=="wait" {fmt.Println("ready");for{time.Sleep(time.Second)}}
 if len(args)>0 && args[0]=="tty" {s,_:=os.Stdin.Stat();fmt.Println(s.Mode()&os.ModeCharDevice!=0);return}
 if len(args)>0 && args[0]=="stream" {io.Copy(os.Stdout,os.Stdin);fmt.Fprintln(os.Stderr,"child-stderr");os.Exit(42)}
 json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"args":args,"source":os.Getenv("SOURCE_FROM"),"caller":os.Getenv("ARKCLI_CALLER_NAME")})
}
'''

def main():
    with tempfile.TemporaryDirectory(prefix="ve-catalog-e2e-") as temp:
        tmp = Path(temp)
        host = tmp / ("ve.exe" if os.name == "nt" else "ve")
        tool = tmp / ("fixture.exe" if os.name == "nt" else "fixture")
        subprocess.run(["go", "build", "-o", str(host), "."], cwd=ROOT, check=True)
        source = tmp / "fixture.go"
        source.write_text(HELPER)
        subprocess.run(["go", "build", "-o", str(tool), str(source)], cwd=ROOT, check=True)
        host_digest = hashlib.sha256(host.read_bytes()).hexdigest()
        home = tmp / "home"
        home.mkdir()
        env = dict(os.environ, HOME=str(home), USERPROFILE=str(home), PATH=str(tmp)+os.pathsep+os.environ["PATH"],
                   VOLCENGINE_CLI_DISABLE_UPDATE_CHECK="1", VE_CALLER_NAME="fixture-agent")
        for name in ["CONFIG_FILE_DIR", "VOLCENGINE_CLI_PROXY_PATH"]:
            env.pop(name, None)
        def run(*args, code=0, input=None):
            p = subprocess.run([str(host), *args], env=env, input=input, text=True, capture_output=True, timeout=15)
            assert p.returncode == code, (args, p.returncode, p.stdout, p.stderr)
            return p
        before = json.loads(run("proxycli", "list", "--output", "json").stdout)
        assert len(before["catalog"]["tools"]) == 24
        run("--help")
        run("proxycli", "info", "arkcli")
        run("completion", "bash")
        assert not (home/".volcengine"/"config.json").exists(), "local commands accessed credentials"
        data = tool.read_bytes()
        platform = subprocess.check_output(["go", "env", "GOOS", "GOARCH"], text=True).split()
        spec = dict(id="fixture-id", command="fixture-new", aliases=["fixture-alias"], description="Offline integration fixture",
                    readiness="ready", release_version="1.0.0", executable="fixture", launch=dict(kind="native"),
                    source_url="https://docs.volcengine.com/docs/83927/1176799", path_policy="prefer",
                    platforms={"-".join(platform): dict(url="https://cloudcache.volccdn.com/ve/fixtures/native",
                        sha256=hashlib.sha256(data).hexdigest(), size=len(data))},
                    skills=dict(repository="https://github.com/volcengine/volcengine-skills", names=[]))
        catalog = dict(schema_version=2, catalog_revision="fixture-process-1", min_installer_protocol=3, tools=[spec])
        pending = dict(spec, id="pending-fixture", command="pending-fixture", aliases=[], readiness="discovered", gaps=["test fixture"], platforms={})
        catalog["tools"].append(pending)
        local = tmp / "catalog.json"
        local.write_text(json.dumps(catalog))
        run("proxycli", "validate", str(local))
        run("proxycli", "refresh", "--file", str(local))
        assert "fixture-new" in run("--help").stdout
        assert "fixture-new" in run("__complete", "fixture").stdout
        assert not (home/".volcengine"/"config.json").exists(), "refresh/completion accessed credentials"
        args = ["--profile", "child-profile", "--region", "child-region", "--endpoint", "space endpoint",
                "--lang", "child-language", "--version", "--", "", "中文", "a b", "--help"]
        for prefix in [("fixture-new",), ("fixture-alias",), ("proxycli", "run", "fixture-id", "--")]:
            output = json.loads(run("--lang", "ZH", *prefix, *args).stdout)
            assert output["args"] == args, output
            stream = run(*prefix, "stream", code=42, input="stdin 中文\n")
            assert stream.stdout == "stdin 中文\n" and "child-stderr" in stream.stderr
        for name in ["arkcli", "tosutil"]:
            shutil.copy2(tool, tmp / (name + (".exe" if os.name == "nt" else "")))
        for name in ["arkcli", "tosutil", "toscli"]:
            output = json.loads(run(name, "--help").stdout)
            assert output["args"] == ["--help"]
            if name == "arkcli":
                assert output["source"] == "ve" and output["caller"] == "fixture-agent"
        assert "pending-fixture" not in run("--help").stdout
        run("proxycli", "run", "pending-fixture", "--", "--help", code=1)
        shutil.copy2(tool, tmp / ("vefaas.exe" if os.name == "nt" else "vefaas"))
        forwarded = json.loads(run("vefaas", "--profile", "child", "--lang", "child-language", "--help").stdout)
        assert forwarded["args"] == ["--profile", "child", "--lang", "child-language", "--help"]
        # Native argv may contain a known OpenAPI action after native flags.
        # Cobra must not steal that token while traversing service children.
        for args in [["--help", "ListFunctions"], ["--profile", "prod", "ListFunctions", "--help"]]:
            forwarded = json.loads(run("vefaas", *args).stdout)
            assert forwarded["args"] == args, forwarded
        api_result = run("vefaas", "ListFunctions", "--help")
        api_help = api_result.stdout + api_result.stderr
        assert "ListFunctions" in api_help and not api_result.stdout.lstrip().startswith("{"), (api_result.stdout, api_result.stderr)
        # Explicit future OpenAPI actions stay on the host path; help keeps this offline.
        forced = run("vefaas", "NewUnlistedAction", "--force", "--help")
        assert "Available Actions" in forced.stdout + forced.stderr
        assert not forced.stdout.lstrip().startswith("{"), forced.stdout
        service_help = run("help", "vefaas")
        assert "ListFunctions" in service_help.stdout + service_help.stderr
        assert not service_help.stdout.lstrip().startswith("{"), service_help.stdout
        run("proxycli", "run", "missing-tool", "--", code=1)
        run("skills", "install", "--tool", "tls-cli", code=1)
        # Root conflicts never override OpenAPI. Stable-id execution still works.
        spec["command"] = "ecs"
        spec["aliases"] = ["configure"]
        catalog["catalog_revision"] = "fixture-process-2"
        local.write_text(json.dumps(catalog))
        run("proxycli", "validate", str(local), code=1)
        run("proxycli", "refresh", "--file", str(local))
        assert "fixture" not in run("ecs", "--help").stdout
        assert json.loads(run("proxycli", "run", "fixture-id", "--", "--help").stdout)["args"] == ["--help"]
        local.write_text('{"schema_version":99}')
        run("proxycli", "refresh", "--file", str(local), code=1)
        run("proxycli", "run", "fixture-id", "--", "--help")
        if os.name != "nt":
            import pty
            import select
            master, slave = pty.openpty()
            p = subprocess.Popen([str(host), "proxycli", "run", "fixture-id", "--", "tty"], env=env,
                                 stdin=slave, stdout=slave, stderr=slave)
            os.close(slave)
            assert select.select([master], [], [], 10)[0], "TTY timeout"
            assert b"true" in os.read(master, 8192)
            assert p.wait(timeout=10) == 0
            os.close(master)
            p = subprocess.Popen([str(host), "proxycli", "run", "fixture-id", "--", "wait"], env=env,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            assert select.select([p.stdout], [], [], 10)[0], "child startup timeout"
            assert p.stdout.readline().strip() == "ready"
            p.send_signal(signal.SIGTERM)
            assert p.wait(timeout=10) == -signal.SIGTERM, "signal/PID handoff changed"
            # Symlink recursion is rejected before any proxy execution.
            tool.unlink()
            tool.symlink_to(host)
            err = run("proxycli", "run", "fixture-id", "--", "--help", code=1).stderr
            assert "recursive" in err
        assert hashlib.sha256(host.read_bytes()).hexdigest() == host_digest
        print("PASS: unchanged host; local refresh; dynamic/alias/stable-id argv; stdio/exit; native conflicts; legacy environment; offline help/completion; failed refresh; TTY/signals; recursion")

if __name__ == "__main__":
    main()
