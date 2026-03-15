from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def append_nix_config(config_line: str) -> None:
    current = os.environ.get("NIX_CONFIG")
    if current:
        os.environ["NIX_CONFIG"] = f"{current}\n{config_line}"
    else:
        os.environ["NIX_CONFIG"] = config_line


append_nix_config("experimental-features = nix-command flakes")


def utc_now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat()


def die(message: str, exit_code: int = 1) -> None:
    raise BackendError(message, exit_code=exit_code)


class BackendError(RuntimeError):
    def __init__(self, message: str, *, exit_code: int = 1):
        super().__init__(message)
        self.exit_code = exit_code


@dataclass(slots=True)
class HostRecord:
    host: str
    user: str
    hostName: str
    initialOutput: str
    finalOutput: str
    needsFinalize: bool
    deferredFeatures: list[str]


@dataclass(slots=True)
class DiskRecord:
    path: str
    preferredPath: str
    size: str
    model: str
    transport: str
    serial: str
    mountpoints: list[str]
    isLiveMedia: bool


def run(
    cmd: list[str],
    *,
    cwd: str | None = None,
    env: dict[str, str] | None = None,
    input_text: str | None = None,
    capture_output: bool = True,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        input=input_text,
        text=True,
        check=False,
        capture_output=capture_output,
    )


def require_ok(
    cmd: list[str],
    *,
    cwd: str | None = None,
    env: dict[str, str] | None = None,
    input_text: str | None = None,
    capture_output: bool = True,
) -> subprocess.CompletedProcess[str]:
    completed = run(
        cmd,
        cwd=cwd,
        env=env,
        input_text=input_text,
        capture_output=capture_output,
    )
    if completed.returncode != 0:
        stderr = completed.stderr.strip()
        stdout = completed.stdout.strip()
        details = stderr or stdout or f"command exited with {completed.returncode}"
        raise BackendError(f"{' '.join(cmd)} failed: {details}", exit_code=completed.returncode)
    return completed


def ensure_tool(tool: str) -> None:
    if shutil.which(tool) is None:
        die(f"required command not found: {tool}")


def normalize_repo_root(path: str) -> str:
    return str(Path(path).expanduser().resolve(strict=False))


def normalize_existing_path(path: str) -> str:
    return str(Path(path).expanduser().resolve(strict=True))


def ensure_flake_repo(repo_root: str) -> None:
    if not Path(repo_root, "flake.nix").is_file():
        die(f"repo checkout not found at {repo_root}")


def is_git_checkout(repo_root: str) -> bool:
    completed = run(["git", "-C", repo_root, "rev-parse", "--show-toplevel"])
    return completed.returncode == 0


def flake_ref_for_repo(repo_root: str) -> str:
    return f"path:{repo_root}"


def git_toplevel_from_cwd() -> str | None:
    completed = run(["git", "rev-parse", "--show-toplevel"])
    if completed.returncode != 0:
        return None
    return normalize_repo_root(completed.stdout.strip())


def assert_expected_repo_revision(repo_root: str) -> None:
    expected_rev = os.environ.get("CONFIG_NIX_BOOTSTRAP_REV", "")
    if not expected_rev or not is_git_checkout(repo_root):
        return

    completed = require_ok(["git", "-C", repo_root, "rev-parse", "HEAD"])
    actual_rev = completed.stdout.strip()
    if actual_rev and actual_rev != expected_rev:
        die(
            f"existing checkout at {repo_root} is at {actual_rev} but installer expects {expected_rev}; "
            f"remove that directory and launch the installer again"
        )


def bootstrap_repo_checkout(repo_root: str) -> tuple[str, str]:
    repo_url = os.environ.get("CONFIG_NIX_BOOTSTRAP_REPO_URL", "")
    repo_rev = os.environ.get("CONFIG_NIX_BOOTSTRAP_REV", "")
    flake_source = os.environ.get("CONFIG_NIX_FLAKE_SOURCE", "")
    parent = Path(repo_root).parent
    parent.mkdir(parents=True, exist_ok=True)

    if repo_url:
        require_ok(["git", "clone", repo_url, repo_root], capture_output=True)
        if repo_rev:
            require_ok(["git", "-C", repo_root, "checkout", repo_rev], capture_output=True)
        return ("temporary", repo_root)

    if flake_source:
        shutil.copytree(flake_source, repo_root, dirs_exist_ok=True)
        for path in Path(repo_root).rglob("*"):
            try:
                path.chmod(path.stat().st_mode | 0o200)
            except OSError:
                pass
        return ("temporary", repo_root)

    die("cannot bootstrap a writable repo checkout automatically")


def prepare_repo_root(requested_repo_root: str | None = None) -> tuple[str, str]:
    if requested_repo_root:
        repo_root = normalize_repo_root(requested_repo_root)
        if Path(repo_root).is_file():
            die(f"{repo_root} exists and is not a directory")
        if Path(repo_root, "flake.nix").is_file():
            ensure_flake_repo(repo_root)
            assert_expected_repo_revision(repo_root)
            return ("explicit", repo_root)
        if Path(repo_root).exists() and any(Path(repo_root).iterdir()):
            die(f"{repo_root} exists but is not a config.nix checkout")
        source_kind, repo_root = bootstrap_repo_checkout(repo_root)
        ensure_flake_repo(repo_root)
        assert_expected_repo_revision(repo_root)
        return (source_kind, repo_root)

    git_root = git_toplevel_from_cwd()
    if git_root and Path(git_root, "flake.nix").is_file():
        ensure_flake_repo(git_root)
        assert_expected_repo_revision(git_root)
        return ("local", git_root)

    temp_root = tempfile.mkdtemp(prefix="config-nix-install.", dir="/tmp")
    source_kind, repo_root = bootstrap_repo_checkout(temp_root)
    ensure_flake_repo(repo_root)
    assert_expected_repo_revision(repo_root)
    return (source_kind, repo_root)


def nix_eval_json(repo_root: str, attribute: str) -> Any:
    completed = require_ok(
        [
            "nix",
            "--extra-experimental-features",
            "nix-command flakes",
            "eval",
            "--json",
            f"{flake_ref_for_repo(repo_root)}#{attribute}",
        ]
    )
    return json.loads(completed.stdout)


def flake_revision_label(repo_root: str) -> str:
    if is_git_checkout(repo_root):
        completed = run(["git", "-C", repo_root, "rev-parse", "--short", "HEAD"])
        if completed.returncode == 0:
            return completed.stdout.strip()
    bootstrap_rev = os.environ.get("CONFIG_NIX_BOOTSTRAP_REV", "")
    return bootstrap_rev[:12] if bootstrap_rev else "unknown"


def live_media_disk() -> str | None:
    source = ""
    for mountpoint in ("/iso", "/run/rootfsbase"):
        completed = run(["findmnt", "-no", "SOURCE", mountpoint])
        if completed.returncode == 0:
            source = completed.stdout.strip()
            if source:
                break
    if not source:
        return None

    try:
        resolved_source = str(Path(source).resolve(strict=True))
    except FileNotFoundError:
        return None

    parent = run(["lsblk", "-ndo", "PKNAME", resolved_source])
    if parent.returncode == 0 and parent.stdout.strip():
        return f"/dev/{parent.stdout.strip()}"
    return resolved_source


def preferred_disk_path(disk_path: str) -> str:
    try:
        resolved = str(Path(disk_path).resolve(strict=True))
    except FileNotFoundError:
        return disk_path

    by_id = Path("/dev/disk/by-id")
    if by_id.is_dir():
        for candidate in sorted(by_id.iterdir()):
            if "-part" in candidate.name:
                continue
            try:
                if str(candidate.resolve(strict=True)) == resolved:
                    return str(candidate)
            except FileNotFoundError:
                continue

    return disk_path


def disk_records() -> list[DiskRecord]:
    completed = require_ok(
        [
            "lsblk",
            "-J",
            "-o",
            "NAME,PATH,SIZE,TYPE,MODEL,VENDOR,SERIAL,TRAN,MOUNTPOINTS",
        ]
    )
    devices = json.loads(completed.stdout)["blockdevices"]
    live_disk = live_media_disk()
    try:
        live_real = str(Path(live_disk).resolve(strict=True)) if live_disk else ""
    except FileNotFoundError:
        live_real = ""

    records: list[DiskRecord] = []
    for device in devices:
        if device.get("type") != "disk":
            continue
        name = device.get("name") or ""
        if name.startswith(("loop", "ram", "zram")):
            continue
        actual_path = device.get("path")
        if not actual_path:
            continue
        try:
            actual_real = str(Path(actual_path).resolve(strict=True))
        except FileNotFoundError:
            actual_real = ""
        mountpoints = [mp for mp in (device.get("mountpoints") or []) if mp]
        model = " ".join(part for part in [device.get("vendor"), device.get("model")] if part).strip()
        records.append(
            DiskRecord(
                path=actual_path,
                preferredPath=preferred_disk_path(actual_path),
                size=device.get("size") or "?",
                model=model or "Unknown device",
                transport=device.get("tran") or "unknown",
                serial=device.get("serial") or "no-serial",
                mountpoints=mountpoints,
                isLiveMedia=bool(live_real and actual_real == live_real),
            )
        )

    return sorted(records, key=lambda record: record.preferredPath)


def preflight_payload(repo_root: str, source_kind: str) -> dict[str, Any]:
    return {
        "uefi": Path("/sys/firmware/efi").is_dir(),
        "revision": flake_revision_label(repo_root),
        "repoRoot": repo_root,
        "sourceKind": source_kind,
        "requiredTools": {
            tool: shutil.which(tool) is not None
            for tool in [
                "age-keygen",
                "disko",
                "findmnt",
                "git",
                "jq",
                "lsblk",
                "mkpasswd",
                "nix",
                "sops",
                "tar",
                "nixos-generate-config",
                "nixos-install",
            ]
        },
    }


def assert_owner_recipients_ready(host: str, meta: dict[str, Any]) -> None:
    recipients = meta.get("ownerAgeRecipients") or []
    if not recipients:
        die(f"hosts/{host}/vars.nix must define at least one ownerAgeRecipients entry")
    placeholder = next((item for item in recipients if "replace" in item.lower()), "")
    if placeholder:
        die(f"replace ownerAgeRecipients in hosts/{host}/vars.nix before running the installer")


def list_hosts_payload(repo_root: str) -> dict[str, Any]:
    meta = nix_eval_json(repo_root, "lib.hostMeta")
    plan = nix_eval_json(repo_root, "lib.installPlan")
    hosts: list[HostRecord] = []

    for host in sorted(meta.keys()):
        host_meta = meta[host]
        assert_owner_recipients_ready(host, host_meta)
        hosts.append(
            HostRecord(
                host=host,
                user=host_meta["user"]["name"],
                hostName=host_meta.get("hostName", host),
                initialOutput=plan[host]["initialOutput"],
                finalOutput=plan[host]["finalOutput"],
                needsFinalize=bool(plan[host]["needsFinalize"]),
                deferredFeatures=list(plan[host]["deferredFeatures"]),
            )
        )

    return {
        "preflight": preflight_payload(repo_root, source_kind="local" if is_git_checkout(repo_root) else "temporary"),
        "hosts": [asdict(host) for host in hosts],
    }


def default_age_key_file() -> str | None:
    env_path = os.environ.get("SOPS_AGE_KEY_FILE")
    if env_path and Path(env_path).is_file():
        return normalize_existing_path(env_path)
    default_path = Path.home() / ".config/sops/age/keys.txt"
    if default_path.is_file():
        return str(default_path.resolve(strict=True))
    return None


def prepare_sops_env(age_key_file: str | None) -> dict[str, str]:
    env = dict(os.environ)
    effective_key = age_key_file or env.get("SOPS_AGE_KEY_FILE") or default_age_key_file()
    if effective_key:
        env["SOPS_AGE_KEY_FILE"] = normalize_existing_path(effective_key)
    return env


def is_sops_file(secret_file: Path) -> bool:
    return secret_file.is_file() and any(line.startswith("sops:") for line in secret_file.read_text().splitlines())


def sops_can_decrypt(repo_root: str, secret_file: Path, env: dict[str, str]) -> bool:
    completed = run(
        ["sops", "--config", str(Path(repo_root) / ".sops.yaml"), "--decrypt", str(secret_file)],
        env=env,
    )
    return completed.returncode == 0


def secret_status_payload(repo_root: str, host: str, age_key_file: str | None) -> dict[str, Any]:
    meta = nix_eval_json(repo_root, f"lib.hostMeta.{host}")
    assert_owner_recipients_ready(host, meta)
    env = prepare_sops_env(age_key_file)
    secret_file = Path(repo_root) / "secrets" / "hosts" / f"{host}.yaml"
    active_key = env.get("SOPS_AGE_KEY_FILE")

    if not secret_file.exists():
        mode = "create"
        decryptable = False
        encrypted = False
    elif not is_sops_file(secret_file):
        mode = "reuse"
        decryptable = True
        encrypted = False
    elif sops_can_decrypt(repo_root, secret_file, env):
        mode = "reuse"
        decryptable = True
        encrypted = True
    else:
        mode = "needs-age-key"
        decryptable = False
        encrypted = True

    return {
        "host": host,
        "mode": mode,
        "encrypted": encrypted,
        "decryptable": decryptable,
        "hasSecret": secret_file.exists(),
        "hostSecretPath": str(secret_file),
        "activeAgeKeyFile": active_key,
        "suggestedAgeKeyFile": default_age_key_file(),
    }


def render_sops_config(repo_root: str, env: dict[str, str]) -> None:
    require_ok(["bash", str(Path(repo_root) / "scripts" / "render-sops-config.sh"), repo_root], env=env)


def stage_install_artifacts(repo_root: str, host: str) -> None:
    stage_paths = [
        ".sops.yaml",
        f"hosts/{host}/hardware-configuration.nix",
        f"secrets/hosts/{host}.age.pub",
        f"secrets/hosts/{host}.yaml",
    ]
    if Path(repo_root, "secrets", "common.yaml").is_file():
        stage_paths.append("secrets/common.yaml")
    require_ok(["git", "-C", repo_root, "add", "--", *stage_paths])


def copy_repo_snapshot(repo_root: str, target_root: str) -> None:
    Path(target_root).mkdir(parents=True, exist_ok=True)
    tar_create = subprocess.Popen(
        [
            "tar",
            "--exclude=./result",
            "--exclude=./result-*",
            "--exclude=./.direnv",
            "-C",
            repo_root,
            "-cf",
            "-",
            ".",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=False,
    )
    tar_extract = subprocess.Popen(
        ["tar", "-C", target_root, "-xf", "-"],
        stdin=tar_create.stdout,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=False,
    )
    if tar_create.stdout is not None:
        tar_create.stdout.close()
    create_stderr = tar_create.communicate()[1]
    extract_stdout, extract_stderr = tar_extract.communicate()
    if tar_create.returncode != 0:
        message = create_stderr.decode().strip() or "tar create failed"
        die(message)
    if tar_extract.returncode != 0:
        message = extract_stderr.decode().strip() or extract_stdout.decode().strip() or "tar extract failed"
        die(message)


def write_json_file(output_path: str, payload: dict[str, Any]) -> None:
    path = Path(output_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n")


def hash_password(password: str) -> str:
    completed = require_ok(
        ["mkpasswd", "--method=yescrypt", "--stdin"],
        input_text=f"{password}\n",
    )
    return completed.stdout.strip()


def write_host_secret(
    repo_root: str,
    host: str,
    *,
    secret_mode: str,
    password: str | None,
    env: dict[str, str],
) -> None:
    host_secret_file = Path(repo_root) / "secrets" / "hosts" / f"{host}.yaml"
    host_secret_file.parent.mkdir(parents=True, exist_ok=True)

    if secret_mode in {"create", "replace"}:
        if not password:
            die("password is required when creating or replacing the host secret")
        host_secret_file.write_text(f'userPasswordHash: "{hash_password(password)}"\n')
        require_ok(
            [
                "sops",
                "--config",
                str(Path(repo_root) / ".sops.yaml"),
                "--encrypt",
                "--in-place",
                str(host_secret_file),
            ],
            env=env,
        )
        return

    if secret_mode != "reuse":
        die(f"unexpected secret mode: {secret_mode}")

    if not host_secret_file.exists():
        die(f"expected existing host secret at {host_secret_file}")

    if is_sops_file(host_secret_file):
        require_ok(
            [
                "sops",
                "--config",
                str(Path(repo_root) / ".sops.yaml"),
                "updatekeys",
                "-y",
                str(host_secret_file),
            ],
            env=env,
        )
    else:
        require_ok(
            [
                "sops",
                "--config",
                str(Path(repo_root) / ".sops.yaml"),
                "--encrypt",
                "--in-place",
                str(host_secret_file),
            ],
            env=env,
        )


def emit(event_type: str, phase: str, message: str, **extra: Any) -> None:
    payload: dict[str, Any] = {
        "type": event_type,
        "phase": phase,
        "message": message,
    }
    payload.update(extra)
    print(json.dumps(payload), flush=True)


def stream_command(cmd: list[str], *, phase: str, env: dict[str, str] | None = None) -> None:
    process = subprocess.Popen(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        env=env,
    )
    assert process.stdout is not None
    for line in process.stdout:
        emit("phase-log", phase, line.rstrip(), rawLine=line.rstrip())
    return_code = process.wait()
    if return_code != 0:
        raise BackendError(f"{' '.join(cmd)} failed with exit code {return_code}", exit_code=return_code)


def write_install_receipt_payload(
    *,
    host: str,
    disk: str,
    initial_output: str,
    final_output: str,
    user: str,
    needs_finalize: bool,
) -> dict[str, Any]:
    return {
        "host": host,
        "disk": disk,
        "initialOutput": initial_output,
        "finalOutput": final_output,
        "repoPath": "/etc/nixos",
        "user": user,
        "installedAt": utc_now(),
        "needsFinalize": needs_finalize,
    }


def assert_safe_install_disk(disk: str) -> str:
    path = Path(disk)
    if not (path.is_block_device() or path.is_symlink()):
        die(f"selected disk is not a block device: {disk}")
    try:
        selected_real = str(path.resolve(strict=True))
    except FileNotFoundError:
        die(f"cannot resolve selected disk: {disk}")

    live_disk = live_media_disk()
    try:
        live_real = str(Path(live_disk).resolve(strict=True)) if live_disk else ""
    except FileNotFoundError:
        live_real = ""
    if live_real and live_real == selected_real:
        die(f"selected disk {disk} appears to be the current boot medium")
    return preferred_disk_path(disk)


def handle_execute(plan_file: str) -> int:
    with open(plan_file, "r", encoding="utf-8") as handle:
        plan = json.load(handle)

    repo_root = normalize_repo_root(plan["repoRoot"])
    host = plan["host"]
    disk = assert_safe_install_disk(plan["disk"])
    mount_point = plan.get("mountPoint") or "/mnt"
    secret_mode = plan["secretMode"]
    password = plan.get("password")
    env = prepare_sops_env(plan.get("ageKeyFile"))

    if not Path("/sys/firmware/efi").is_dir():
        die("installer requires UEFI mode; /sys/firmware/efi is missing")

    required_tools = [
        "age-keygen",
        "disko",
        "findmnt",
        "git",
        "jq",
        "lsblk",
        "mkpasswd",
        "nix",
        "sops",
        "tar",
        "nixos-generate-config",
        "nixos-install",
    ]
    for tool in required_tools:
        ensure_tool(tool)

    meta = nix_eval_json(repo_root, f"lib.hostMeta.{host}")
    install_plan = nix_eval_json(repo_root, f"lib.installPlan.{host}")
    assert_owner_recipients_ready(host, meta)

    user_name = meta["user"]["name"]
    initial_output = install_plan["initialOutput"]
    final_output = install_plan["finalOutput"]
    needs_finalize = bool(install_plan["needsFinalize"])

    host_dir = Path(repo_root) / "hosts" / host
    host_secret_file = Path(repo_root) / "secrets" / "hosts" / f"{host}.yaml"
    host_pub_file = Path(repo_root) / "secrets" / "hosts" / f"{host}.age.pub"
    if secret_mode in {"create", "replace"} and not password:
        die("password is required when creating or replacing the host secret")

    tmpdir = tempfile.mkdtemp(prefix="config-nix-exec.", dir="/tmp")
    disko_config = Path(tmpdir) / "disko-config.nix"
    disko_config.write_text(f'(import "{repo_root}/hosts/{host}/disko.nix" {{\n  diskDevice = "{disk}";\n}})\n')

    try:
        emit("phase-start", "prepare", "Validating install plan and rendering the disko configuration")

        Path(mount_point).mkdir(parents=True, exist_ok=True)
        disko_env = dict(env)
        disko_env["DISKO_ROOT_MOUNTPOINT"] = mount_point
        emit("phase-complete", "prepare", f"Using host {host} and target disk {disk}")

        emit("phase-start", "partition", f"Partitioning and mounting {disk}")
        stream_command(["disko", "--mode", "destroy,format,mount", str(disko_config)], phase="partition", env=disko_env)
        emit("phase-complete", "partition", f"Mounted target filesystem at {mount_point}")

        emit("phase-start", "hardware", "Generating hardware-configuration.nix")
        hardware = require_ok(
            [
                "nixos-generate-config",
                "--root",
                mount_point,
                "--no-filesystems",
                "--show-hardware-config",
            ]
        ).stdout
        (host_dir / "hardware-configuration.nix").write_text(hardware)
        emit("phase-complete", "hardware", f"Wrote hosts/{host}/hardware-configuration.nix")

        emit("phase-start", "host-key", "Creating the target host age key")
        host_key_dir = Path(mount_point) / "var/lib/sops-nix"
        host_key_dir.mkdir(parents=True, exist_ok=True)
        require_ok(["chmod", "700", str(host_key_dir)])
        require_ok(["age-keygen", "-o", str(host_key_dir / "key.txt")], capture_output=True)
        require_ok(["chmod", "600", str(host_key_dir / "key.txt")])
        host_pub = require_ok(["age-keygen", "-y", str(host_key_dir / "key.txt")]).stdout.strip()
        host_pub_file.parent.mkdir(parents=True, exist_ok=True)
        host_pub_file.write_text(f"{host_pub}\n")
        emit("phase-complete", "host-key", f"Wrote secrets/hosts/{host}.age.pub")

        emit("phase-start", "secrets", "Rendering sops rules and preparing host secrets")
        render_sops_config(repo_root, env)
        write_host_secret(repo_root, host, secret_mode=secret_mode, password=password, env=env)
        common_secret = Path(repo_root) / "secrets" / "common.yaml"
        if is_sops_file(common_secret):
            require_ok(
                [
                    "sops",
                    "--config",
                    str(Path(repo_root) / ".sops.yaml"),
                    "updatekeys",
                    "-y",
                    str(common_secret),
                ],
                env=env,
            )
        if not is_git_checkout(repo_root):
            die(f"installer requires a git checkout at {repo_root}")
        stage_install_artifacts(repo_root, host)
        emit("phase-complete", "secrets", f"Staged generated files for {host}")

        emit("phase-start", "persist", "Copying the full git checkout into /etc/nixos")
        target_repo_root = Path(mount_point) / "etc/nixos"
        copy_repo_snapshot(repo_root, str(target_repo_root))

        state_dir = Path(mount_point) / "var/lib/config-nix"
        state_dir.mkdir(parents=True, exist_ok=True)
        receipt = write_install_receipt_payload(
            host=host,
            disk=disk,
            initial_output=initial_output,
            final_output=final_output,
            user=user_name,
            needs_finalize=needs_finalize,
        )
        write_json_file(str(state_dir / "install-receipt.json"), receipt)
        if needs_finalize:
            (state_dir / "finalize-pending").touch()
            write_json_file(
                str(state_dir / "finalize-status.json"),
                {
                    "host": host,
                    "status": "pending",
                    "updatedAt": utc_now(),
                },
            )
        emit("phase-complete", "persist", "Persisted /etc/nixos and wrote install state files")

        emit("phase-start", "install", f"Installing NixOS output {initial_output}")
        stream_command(
            [
                "nixos-install",
                "--root",
                mount_point,
                "--flake",
                f"path:{target_repo_root}#{initial_output}",
            ],
            phase="install",
            env=env,
        )
        emit("phase-complete", "install", f"Installed {initial_output}")

        emit(
            "install-complete",
            "complete",
            "Installation completed successfully",
            host=host,
            disk=disk,
            initialOutput=initial_output,
            finalOutput=final_output,
            needsFinalize=needs_finalize,
            repoPath="/etc/nixos",
            receiptPath="/var/lib/config-nix/install-receipt.json",
        )
        return 0
    except BackendError as error:
        emit("phase-failed", "error", str(error))
        return error.exit_code
    finally:
        shutil.rmtree(tmpdir, ignore_errors=True)


def handle_list_hosts(repo: str | None) -> dict[str, Any]:
    source_kind, repo_root = prepare_repo_root(repo)
    payload = list_hosts_payload(repo_root)
    payload["preflight"]["sourceKind"] = source_kind
    payload["repoRoot"] = repo_root
    return payload


def handle_list_disks() -> dict[str, Any]:
    return {
        "disks": [asdict(record) for record in disk_records()],
    }


def handle_secret_status(repo: str | None, host: str, age_key_file: str | None) -> dict[str, Any]:
    if not repo:
        die("--repo is required for secret-status")
    _, repo_root = prepare_repo_root(repo)
    payload = secret_status_payload(repo_root, host, age_key_file)
    payload["repoRoot"] = repo_root
    return payload


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="install-host-backend")
    subparsers = parser.add_subparsers(dest="command", required=True)

    list_hosts = subparsers.add_parser("list-hosts")
    list_hosts.add_argument("--repo")

    subparsers.add_parser("list-disks")

    secret_status = subparsers.add_parser("secret-status")
    secret_status.add_argument("--repo")
    secret_status.add_argument("--host", required=True)
    secret_status.add_argument("--age-key-file")

    execute = subparsers.add_parser("execute")
    execute.add_argument("--plan-file", required=True)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    try:
        if args.command == "list-hosts":
            print(json.dumps(handle_list_hosts(args.repo)))
            return 0
        if args.command == "list-disks":
            print(json.dumps(handle_list_disks()))
            return 0
        if args.command == "secret-status":
            print(json.dumps(handle_secret_status(args.repo, args.host, args.age_key_file)))
            return 0
        if args.command == "execute":
            return handle_execute(args.plan_file)
    except BackendError as error:
        print(json.dumps({"type": "error", "message": str(error)}))
        return error.exit_code

    parser.error("unknown command")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
