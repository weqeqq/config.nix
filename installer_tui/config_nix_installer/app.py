from __future__ import annotations

import json
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Horizontal, Vertical
from textual.widgets import (
    Button,
    Checkbox,
    ContentSwitcher,
    Footer,
    Input,
    Label,
    ListItem,
    ListView,
    LoadingIndicator,
    ProgressBar,
    RichLog,
    Static,
)


@dataclass(slots=True)
class InstallerState:
    repo_root: str | None = None
    revision: str = "unknown"
    source_kind: str = "temporary"
    uefi: bool = False
    required_tools: dict[str, bool] = field(default_factory=dict)
    hosts: list[dict[str, Any]] = field(default_factory=list)
    disks: list[dict[str, Any]] = field(default_factory=list)
    selected_host: dict[str, Any] | None = None
    selected_disk: dict[str, Any] | None = None
    secret_status: dict[str, Any] | None = None
    secret_mode: str | None = None
    age_key_file: str | None = None
    password: str | None = None
    install_result: dict[str, Any] | None = None
    install_started: bool = False
    install_failed: bool = False
    install_error: str | None = None
    raw_logs: list[str] = field(default_factory=list)
    phase_messages: dict[str, str] = field(default_factory=dict)
    phase_statuses: dict[str, str] = field(default_factory=dict)


class InstallerApp(App[None]):
    TITLE = "config.nix Installer"
    SUB_TITLE = "Fullscreen host installer"
    CSS = """
    Screen {
      background: #111417;
      color: #f2ece4;
    }

    #layout {
      layout: horizontal;
      padding: 1 2;
      height: 1fr;
    }

    #steps {
      width: 25;
      border: round #ffb454;
      background: #171c20;
      padding: 1 1;
    }

    #content-column {
      width: 1fr;
      margin: 0 1;
    }

    #hero {
      border: round #2a3238;
      background: #171c20;
      padding: 1 2;
      height: 5;
      color: #ffcf88;
      text-style: bold;
    }

    #content {
      border: round #46505a;
      background: #13181c;
      padding: 1 2;
      height: 1fr;
    }

    #actions {
      dock: bottom;
      height: 3;
      padding-top: 1;
    }

    #side {
      width: 38;
      border: round #35b0bf;
      background: #171c20;
      padding: 1 1;
    }

    .panel-title {
      color: #ffb454;
      text-style: bold;
      margin-bottom: 1;
    }

    .side-title {
      color: #35b0bf;
      text-style: bold;
      margin-bottom: 1;
    }

    .step-item {
      padding: 0 1;
      margin-bottom: 1;
      color: #7d8790;
      border-left: wide #20262b;
    }

    .step-current {
      color: #ffb454;
      text-style: bold;
      border-left: wide #ffb454;
      background: #20262b;
    }

    .step-done {
      color: #74d0c3;
      border-left: wide #74d0c3;
    }

    Button {
      min-width: 14;
      margin-right: 1;
    }

    Button.primary {
      background: #ffb454;
      color: #111417;
      text-style: bold;
    }

    Button.subtle {
      background: #2a3238;
      color: #f2ece4;
    }

    ListView {
      border: round #46505a;
      background: #0f1316;
      height: 1fr;
      margin: 1 0;
    }

    Input {
      margin: 1 0;
    }

    Checkbox {
      margin: 1 0;
    }

    ProgressBar {
      margin: 1 0;
    }

    #summary-view, #logs-view {
      height: 1fr;
    }

    RichLog {
      border: round #46505a;
      background: #0d1114;
      height: 1fr;
    }

    .muted {
      color: #9aa3ab;
    }

    .warning {
      color: #ffb454;
    }

    .error {
      color: #ff8170;
      text-style: bold;
    }
    """

    BINDINGS = [
        Binding("q", "quit_or_ignore", "Quit", show=True),
        Binding("escape", "back", "Back", show=True),
        Binding("enter", "advance", "Continue", show=True),
        Binding("l", "toggle_logs", "Logs", show=True),
    ]

    STEPS = [
        ("preflight", "Preflight"),
        ("host", "Host"),
        ("disk", "Disk"),
        ("secret", "Secrets"),
        ("confirm", "Confirm"),
        ("install", "Install"),
        ("complete", "Complete"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self.state = InstallerState()
        self.current_step = "preflight"
        self.show_raw_logs = False
        self._step_history: list[str] = []
        self._phase_order = ["prepare", "partition", "hardware", "host-key", "secrets", "persist", "install"]
        self._ephemeral_plan_file: str | None = None

    def compose(self) -> ComposeResult:
        with Horizontal(id="layout"):
            with Vertical(id="steps"):
                yield Static("config.nix", classes="panel-title")
                yield Static("One command.\nFull-screen install.", classes="muted")
                for key, label in self.STEPS:
                    yield Static(label, id=f"step-{key}", classes="step-item")
            with Vertical(id="content-column"):
                yield Static("Preparing installer session", id="hero")
                with Vertical(id="content"):
                    with ContentSwitcher(initial="panel-preflight", id="content-switcher"):
                        with Vertical(id="panel-preflight"):
                            yield Static("Environment", classes="panel-title")
                            yield Static("Bootstrapping a writable repository checkout and collecting host metadata.", id="preflight-body")
                            yield LoadingIndicator(id="preflight-spinner")
                        with Vertical(id="panel-host"):
                            yield Static("Select Host", classes="panel-title")
                            yield Static("Choose one of the existing host profiles from the repository.", id="host-body")
                            yield Container(id="host-list-holder")
                        with Vertical(id="panel-disk"):
                            yield Static("Select Disk", classes="panel-title")
                            yield Static("Choose the installation target. The live ISO boot disk is marked and hidden from the default flow.", id="disk-body")
                            yield Container(id="disk-list-holder")
                        with Vertical(id="panel-secret"):
                            yield Static("Resolve Secrets", classes="panel-title")
                            yield Static("", id="secret-body")
                            yield Input(placeholder="Age key file (optional)", id="age-key-input")
                            yield Checkbox("Replace the existing encrypted host secret with a new password", id="replace-secret")
                            yield Input(placeholder="Initial password", password=True, id="password-input")
                            yield Input(placeholder="Confirm password", password=True, id="password-confirm-input")
                            yield Static("", id="secret-hint", classes="muted")
                        with Vertical(id="panel-confirm"):
                            yield Static("Final Confirmation", classes="panel-title")
                            yield Static("", id="confirm-body")
                            yield Input(placeholder="Type erase to continue", id="confirm-input")
                        with Vertical(id="panel-install"):
                            yield Static("Installing", classes="panel-title")
                            yield Static("The installer is now applying the destructive plan. Raw logs can be toggled with `l`.", id="install-body")
                            yield ProgressBar(total=len(self._phase_order), id="install-progress")
                            yield Static("", id="install-curated")
                        with Vertical(id="panel-complete"):
                            yield Static("Completed", classes="panel-title")
                            yield Static("", id="complete-body")
                    with Horizontal(id="actions"):
                        yield Button("Back", id="back", classes="subtle")
                        yield Button("Continue", id="continue", classes="primary")
                        yield Button("Quit", id="quit", classes="subtle")
            with Vertical(id="side"):
                yield Static("Session", classes="side-title")
                with ContentSwitcher(initial="summary-view", id="side-switcher"):
                    with Vertical(id="summary-view"):
                        yield Static("", id="summary-body")
                    with Vertical(id="logs-view"):
                        yield Static("Raw Logs", classes="side-title")
                        yield RichLog(id="raw-log", wrap=True, highlight=True, auto_scroll=True)
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#age-key-input", Input).value = ""
        self.query_one("#replace-secret", Checkbox).value = False
        self._sync_step_visuals()
        self._sync_actions()
        self._sync_side_panel()
        self._sync_summary()
        self._bootstrap_session()

    def backend_path(self) -> str:
        local = Path(sys.argv[0]).resolve().with_name("install-host-backend")
        if local.exists():
            return str(local)
        backend = shutil.which("install-host-backend")
        if backend:
            return backend
        raise RuntimeError("install-host-backend was not found next to the UI launcher")

    def run_backend_json(self, args: list[str]) -> dict[str, Any]:
        completed = subprocess.run(
            [self.backend_path(), *args],
            check=False,
            capture_output=True,
            text=True,
        )
        if completed.returncode != 0:
            try:
                payload = json.loads(completed.stdout or "{}")
            except json.JSONDecodeError:
                payload = {}
            message = payload.get("message") or completed.stderr.strip() or completed.stdout.strip() or f"backend command failed with {completed.returncode}"
            raise RuntimeError(message)
        return json.loads(completed.stdout)

    @work(thread=True)
    def _bootstrap_session(self) -> None:
        try:
            payload = self.run_backend_json(["list-hosts"])
            disks = self.run_backend_json(["list-disks"])
        except Exception as error:
            self.call_from_thread(self._set_bootstrap_error, str(error))
            return

        self.call_from_thread(self._apply_bootstrap_payload, payload, disks)

    def _set_bootstrap_error(self, message: str) -> None:
        self.state.install_failed = True
        self.state.install_error = message
        self.query_one("#preflight-body", Static).update(f"Bootstrap failed.\n{message}")
        self.query_one("#hero", Static).update("Installer bootstrap failed")
        self.query_one("#continue", Button).disabled = True
        self.query_one("#back", Button).disabled = True

    def _apply_bootstrap_payload(self, payload: dict[str, Any], disk_payload: dict[str, Any]) -> None:
        preflight = payload["preflight"]
        self.state.repo_root = payload["repoRoot"]
        self.state.revision = preflight["revision"]
        self.state.source_kind = preflight["sourceKind"]
        self.state.uefi = bool(preflight["uefi"])
        self.state.required_tools = preflight["requiredTools"]
        self.state.hosts = payload["hosts"]
        self.state.disks = [disk for disk in disk_payload["disks"] if not disk["isLiveMedia"]]

        tools_ready = all(self.state.required_tools.values())
        preflight_lines = [
            f"UEFI: {'yes' if self.state.uefi else 'no'}",
            f"Revision: {self.state.revision}",
            f"Repository: {self.state.repo_root}",
            f"Source: {self.state.source_kind}",
            f"Required tools: {'ready' if tools_ready else 'missing'}",
        ]
        self.query_one("#preflight-body", Static).update("\n".join(preflight_lines))
        self.query_one("#hero", Static).update(f"Revision {self.state.revision}  |  {self.state.source_kind} checkout")
        self._populate_host_list()
        self._populate_disk_list()

        if not self.state.uefi:
            self._set_bootstrap_error("The installer requires UEFI mode.")
            return
        if not tools_ready:
            missing = ", ".join(sorted(tool for tool, ready in self.state.required_tools.items() if not ready))
            self._set_bootstrap_error(f"Missing required tools: {missing}")
            return
        if not self.state.hosts:
            self._set_bootstrap_error("No installable hosts were found under hosts/.")
            return
        if len(self.state.hosts) == 1:
            self.state.selected_host = self.state.hosts[0]
            self._go_to_step("disk")
        else:
            self._go_to_step("host")
        self._sync_summary()

    def _populate_host_list(self) -> None:
        holder = self.query_one("#host-list-holder", Container)
        items = []
        for index, host in enumerate(self.state.hosts):
            badge = "Deferred finalize" if host["needsFinalize"] else "Direct install"
            subtitle = f"{host['host']}  |  {host['user']}  |  {badge}"
            items.append(ListItem(Label(subtitle), id=f"host-{index}"))
        holder.remove_children()
        list_view = ListView(*items, id="host-list")
        holder.mount(list_view)
        if list_view.children:
            list_view.index = 0
            self.set_focus(list_view)

    def _populate_disk_list(self) -> None:
        holder = self.query_one("#disk-list-holder", Container)
        items = []
        for index, disk in enumerate(self.state.disks):
            label = f"{disk['preferredPath']}  |  {disk['size']}  |  {disk['transport']}  |  {disk['model']}"
            if disk["mountpoints"]:
                label = f"{label}  |  mounted: {', '.join(disk['mountpoints'])}"
            items.append(ListItem(Label(label), id=f"disk-{index}"))
        holder.remove_children()
        list_view = ListView(*items, id="disk-list")
        holder.mount(list_view)
        if list_view.children:
            list_view.index = 0

    def _go_to_step(self, step: str) -> None:
        if step != self.current_step:
            if self.current_step not in {"install", "complete"}:
                self._step_history.append(self.current_step)
        self.current_step = step
        self.query_one("#content-switcher", ContentSwitcher).current = f"panel-{step}"
        self._sync_step_visuals()
        self._sync_actions()
        self._sync_side_panel()
        self._sync_summary()
        self._refresh_step_body()

    def _sync_step_visuals(self) -> None:
        current_index = next(index for index, item in enumerate(self.STEPS) if item[0] == self.current_step)
        for index, (key, _) in enumerate(self.STEPS):
            widget = self.query_one(f"#step-{key}", Static)
            widget.remove_class("step-current")
            widget.remove_class("step-done")
            if index < current_index:
                widget.add_class("step-done")
            if key == self.current_step:
                widget.add_class("step-current")

    def _sync_actions(self) -> None:
        back = self.query_one("#back", Button)
        cont = self.query_one("#continue", Button)
        quit_button = self.query_one("#quit", Button)

        back.disabled = self.current_step in {"preflight", "install"} or self.state.install_started
        quit_button.disabled = self.current_step == "install" and self.state.install_started

        if self.current_step == "confirm":
            cont.label = "Install"
            cont.disabled = False
        elif self.current_step == "install":
            cont.label = "Continue"
            cont.disabled = True
        elif self.current_step == "complete":
            cont.label = "Close"
            cont.disabled = False
        else:
            cont.label = "Continue"
            cont.disabled = self.current_step == "preflight"

    def _sync_side_panel(self) -> None:
        switcher = self.query_one("#side-switcher", ContentSwitcher)
        if self.current_step == "install" and self.show_raw_logs:
            switcher.current = "logs-view"
        else:
            switcher.current = "summary-view"

    def _summary_text(self) -> str:
        host = self.state.selected_host["host"] if self.state.selected_host else "Not selected"
        disk = self.state.selected_disk["preferredPath"] if self.state.selected_disk else "Not selected"
        secret_line = "Pending"
        if self.state.secret_mode == "reuse":
            secret_line = "Reuse existing host secret"
        elif self.state.secret_mode == "create":
            secret_line = "Create new host secret"
        elif self.state.secret_mode == "replace":
            secret_line = "Replace host secret"

        lines = [
            "Repository",
            self.state.repo_root or "Preparing...",
            "",
            "Selections",
            f"Host: {host}",
            f"Disk: {disk}",
            f"Secrets: {secret_line}",
        ]
        if self.state.selected_host:
            lines.extend(
                [
                    f"Install output: {self.state.selected_host['initialOutput']}",
                    f"Final output: {self.state.selected_host['finalOutput']}",
                    f"Deferred finalize: {'yes' if self.state.selected_host['needsFinalize'] else 'no'}",
                ]
            )
        if self.current_step == "install":
            lines.extend(["", "Hints", "Press l to toggle raw logs"])
        return "\n".join(lines)

    def _sync_summary(self) -> None:
        self.query_one("#summary-body", Static).update(self._summary_text())

    def _refresh_step_body(self) -> None:
        if self.current_step == "secret":
            self._refresh_secret_step()
        elif self.current_step == "confirm":
            self._refresh_confirm_step()
        elif self.current_step == "install":
            self._refresh_install_step()
        elif self.current_step == "complete":
            self._refresh_complete_step()

    def _refresh_secret_step(self) -> None:
        body = self.query_one("#secret-body", Static)
        hint = self.query_one("#secret-hint", Static)
        age_input = self.query_one("#age-key-input", Input)
        replace_checkbox = self.query_one("#replace-secret", Checkbox)
        password_input = self.query_one("#password-input", Input)
        confirm_input = self.query_one("#password-confirm-input", Input)

        if self.state.secret_status is None:
            body.update("Detecting host secret status...")
            hint.update("")
            return

        mode = self.state.secret_status["mode"]
        suggested = self.state.secret_status.get("suggestedAgeKeyFile") or ""
        if not age_input.value and suggested:
            age_input.value = suggested

        if mode == "reuse":
            body.update("A decryptable host secret already exists. The installer will reuse it and skip the password prompt.")
            hint.update(self.state.secret_status.get("activeAgeKeyFile") or "No age key was required for reuse.")
            replace_checkbox.disabled = True
            password_input.disabled = True
            confirm_input.disabled = True
            age_input.disabled = True
            self.state.secret_mode = "reuse"
        elif mode == "create":
            body.update("No host secret exists yet. Enter the initial user password to create one.")
            hint.update("Use a simple ASCII password on VMs if you want the least surprising login flow.")
            replace_checkbox.disabled = True
            password_input.disabled = False
            confirm_input.disabled = False
            age_input.disabled = True
            self.state.secret_mode = "create"
        else:
            body.update("An encrypted host secret already exists, but the current age key cannot decrypt it. Provide a valid age key file or replace the host secret.")
            hint.update("If you choose replacement, the existing encrypted host password will be overwritten.")
            replace_checkbox.disabled = False
            age_input.disabled = False
            replacing = replace_checkbox.value
            password_input.disabled = not replacing
            confirm_input.disabled = not replacing
            self.state.secret_mode = "replace" if replacing else None

    def _refresh_confirm_step(self) -> None:
        host = self.state.selected_host or {}
        disk = self.state.selected_disk or {}
        secret_mode = self.state.secret_mode or "pending"
        self.query_one("#confirm-body", Static).update(
            "\n".join(
                [
                    f"Host: {host.get('host', '?')}",
                    f"User: {host.get('user', '?')}",
                    f"Disk: {disk.get('preferredPath', '?')}",
                    f"Install output: {host.get('initialOutput', '?')}",
                    f"Final output: {host.get('finalOutput', '?')}",
                    f"Secrets: {secret_mode}",
                    "",
                    "This will wipe the selected disk via disko.",
                ]
            )
        )

    def _curated_progress_text(self) -> str:
        lines = []
        for phase in self._phase_order:
            status = self.state.phase_statuses.get(phase, "pending")
            message = self.state.phase_messages.get(phase, "")
            prefix = {
                "pending": "--",
                "running": ">>",
                "done": "OK",
                "failed": "!!",
            }.get(status, "--")
            label = phase.replace("-", " ").title()
            lines.append(f"{prefix} {label}")
            if message:
                lines.append(f"   {message}")
        if self.state.install_error:
            lines.extend(["", f"Failure: {self.state.install_error}"])
        return "\n".join(lines)

    def _refresh_install_step(self) -> None:
        self.query_one("#install-curated", Static).update(self._curated_progress_text())
        progress = self.query_one("#install-progress", ProgressBar)
        completed = sum(1 for phase in self._phase_order if self.state.phase_statuses.get(phase) == "done")
        progress.update(progress=completed)

    def _refresh_complete_step(self) -> None:
        result = self.state.install_result or {}
        finalize_line = (
            "First boot will finalize the install, prepare Secure Boot material, and reboot once more."
            if result.get("needsFinalize")
            else "No first-boot finalization is required."
        )
        self.query_one("#complete-body", Static).update(
            "\n".join(
                [
                    f"Installed output: {result.get('initialOutput', '?')}",
                    f"Final output: {result.get('finalOutput', '?')}",
                    f"Canonical repo: {result.get('repoPath', '/etc/nixos')}",
                    f"Receipt: {result.get('receiptPath', '/var/lib/config-nix/install-receipt.json')}",
                    "",
                    finalize_line,
                    "",
                    "Remove the ISO or move the disk ahead of the ISO in firmware boot order before the next boot.",
                    "Generated install files are already staged in git inside /etc/nixos.",
                ]
            )
        )

    def _selected_index(self, list_view: ListView, prefix: str) -> int:
        if list_view.index is None or list_view.index < 0:
            raise RuntimeError("No item is selected.")
        item = list_view.children[list_view.index]
        identifier = item.id or ""
        if not identifier.startswith(prefix):
            raise RuntimeError("Unexpected selection state.")
        return int(identifier[len(prefix):])

    def action_quit_or_ignore(self) -> None:
        if self.current_step == "install" and self.state.install_started:
            return
        self.exit()

    def action_back(self) -> None:
        if self.current_step in {"preflight", "install"} or self.state.install_started:
            return
        if not self._step_history:
            return
        previous = self._step_history.pop()
        self.current_step = previous
        self.query_one("#content-switcher", ContentSwitcher).current = f"panel-{previous}"
        self._sync_step_visuals()
        self._sync_actions()
        self._sync_side_panel()
        self._sync_summary()
        self._refresh_step_body()

    def action_toggle_logs(self) -> None:
        if self.current_step != "install":
            return
        self.show_raw_logs = not self.show_raw_logs
        self._sync_side_panel()

    def action_advance(self) -> None:
        self._handle_continue()

    @on(Button.Pressed, "#back")
    def _on_back_pressed(self) -> None:
        self.action_back()

    @on(Button.Pressed, "#quit")
    def _on_quit_pressed(self) -> None:
        self.action_quit_or_ignore()

    @on(Button.Pressed, "#continue")
    def _on_continue_pressed(self) -> None:
        self._handle_continue()

    @on(Checkbox.Changed, "#replace-secret")
    def _on_replace_secret_changed(self) -> None:
        self._refresh_secret_step()

    def _handle_continue(self) -> None:
        try:
            if self.current_step == "host":
                list_view = self.query_one("#host-list", ListView)
                self.state.selected_host = self.state.hosts[self._selected_index(list_view, "host-")]
                self._go_to_step("disk")
                return

            if self.current_step == "disk":
                list_view = self.query_one("#disk-list", ListView)
                self.state.selected_disk = self.state.disks[self._selected_index(list_view, "disk-")]
                self._load_secret_status()
                return

            if self.current_step == "secret":
                self._continue_from_secret()
                return

            if self.current_step == "confirm":
                phrase = self.query_one("#confirm-input", Input).value.strip().lower()
                if phrase != "erase":
                    raise RuntimeError("Type `erase` to confirm the destructive install.")
                self._start_install()
                return

            if self.current_step == "complete":
                self.exit()
                return
        except RuntimeError as error:
            self.notify(str(error), severity="error")

    @work(thread=True)
    def _load_secret_status(self) -> None:
        assert self.state.repo_root is not None
        assert self.state.selected_host is not None
        try:
            payload = self.run_backend_json(
                [
                    "secret-status",
                    "--repo",
                    self.state.repo_root,
                    "--host",
                    self.state.selected_host["host"],
                ]
            )
        except Exception as error:
            self.call_from_thread(self.notify, str(error), severity="error")
            return
        self.call_from_thread(self._apply_secret_status, payload)

    def _apply_secret_status(self, payload: dict[str, Any]) -> None:
        self.state.secret_status = payload
        self.state.age_key_file = payload.get("activeAgeKeyFile") or payload.get("suggestedAgeKeyFile")
        self._go_to_step("secret")

    def _continue_from_secret(self) -> None:
        if self.state.secret_status is None:
            raise RuntimeError("Secret status is still loading.")

        mode = self.state.secret_status["mode"]
        age_key_input = self.query_one("#age-key-input", Input).value.strip()
        replace_secret = self.query_one("#replace-secret", Checkbox).value
        password = self.query_one("#password-input", Input).value
        confirm = self.query_one("#password-confirm-input", Input).value

        if mode == "reuse":
            self.state.secret_mode = "reuse"
            self.state.password = None
            self._go_to_step("confirm")
            return

        if mode == "create":
            if not password:
                raise RuntimeError("Password cannot be empty.")
            if password != confirm:
                raise RuntimeError("Passwords do not match.")
            self.state.secret_mode = "create"
            self.state.password = password
            self._go_to_step("confirm")
            return

        if replace_secret:
            if not password:
                raise RuntimeError("Password cannot be empty when replacing the host secret.")
            if password != confirm:
                raise RuntimeError("Passwords do not match.")
            self.state.secret_mode = "replace"
            self.state.password = password
            self.state.age_key_file = None
            self._go_to_step("confirm")
            return

        if not age_key_input:
            raise RuntimeError("Provide an age key file or enable secret replacement.")

        self.state.age_key_file = age_key_input
        self._retry_secret_status_with_key(age_key_input)

    @work(thread=True)
    def _retry_secret_status_with_key(self, age_key_file: str) -> None:
        assert self.state.repo_root is not None
        assert self.state.selected_host is not None
        try:
            payload = self.run_backend_json(
                [
                    "secret-status",
                    "--repo",
                    self.state.repo_root,
                    "--host",
                    self.state.selected_host["host"],
                    "--age-key-file",
                    age_key_file,
                ]
            )
        except Exception as error:
            self.call_from_thread(self.notify, str(error), severity="error")
            return
        self.call_from_thread(self._apply_retried_secret_status, payload, age_key_file)

    def _apply_retried_secret_status(self, payload: dict[str, Any], age_key_file: str) -> None:
        self.state.secret_status = payload
        if payload["mode"] != "reuse":
            self.notify("That age key could not decrypt the existing host secret.", severity="error")
            self._refresh_secret_step()
            return
        self.state.age_key_file = age_key_file
        self.state.secret_mode = "reuse"
        self.state.password = None
        self._go_to_step("confirm")

    def _start_install(self) -> None:
        assert self.state.repo_root is not None
        assert self.state.selected_host is not None
        assert self.state.selected_disk is not None
        assert self.state.secret_mode is not None

        plan = {
            "repoRoot": self.state.repo_root,
            "host": self.state.selected_host["host"],
            "disk": self.state.selected_disk["preferredPath"],
            "mountPoint": "/mnt",
            "ageKeyFile": self.state.age_key_file,
            "secretMode": self.state.secret_mode,
            "password": self.state.password,
        }

        temp = tempfile.NamedTemporaryFile(prefix="config-nix-plan.", suffix=".json", delete=False)
        with temp:
            temp.write(json.dumps(plan).encode())
        self._ephemeral_plan_file = temp.name

        self.state.install_started = True
        self.state.install_failed = False
        self.state.install_error = None
        self.state.phase_messages.clear()
        self.state.phase_statuses.clear()
        self.state.raw_logs.clear()
        self.query_one("#raw-log", RichLog).clear()
        self.show_raw_logs = False
        self._go_to_step("install")
        self._run_install(plan_file=temp.name)

    @work(thread=True)
    def _run_install(self, plan_file: str) -> None:
        process = subprocess.Popen(
            [self.backend_path(), "execute", "--plan-file", plan_file],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        assert process.stdout is not None
        for line in process.stdout:
            stripped = line.strip()
            if not stripped:
                continue
            try:
                payload = json.loads(stripped)
            except json.JSONDecodeError:
                self.call_from_thread(self._append_raw_log, stripped)
                continue
            self.call_from_thread(self._apply_install_event, payload)

        stderr = process.stderr.read() if process.stderr is not None else ""
        return_code = process.wait()
        if return_code != 0 and stderr.strip():
            self.call_from_thread(self._append_raw_log, stderr.strip())
        self.call_from_thread(self._finish_install, return_code)

    def _append_raw_log(self, line: str) -> None:
        self.state.raw_logs.append(line)
        self.query_one("#raw-log", RichLog).write(line)

    def _apply_install_event(self, payload: dict[str, Any]) -> None:
        event_type = payload.get("type")
        phase = payload.get("phase", "")
        message = payload.get("message", "")

        if payload.get("rawLine"):
            self._append_raw_log(payload["rawLine"])

        if phase in self._phase_order and event_type == "phase-start":
            self.state.phase_statuses[phase] = "running"
            self.state.phase_messages[phase] = message
        elif phase in self._phase_order and event_type == "phase-complete":
            self.state.phase_statuses[phase] = "done"
            self.state.phase_messages[phase] = message
        elif event_type == "phase-failed":
            if phase in self._phase_order:
                self.state.phase_statuses[phase] = "failed"
            self.state.install_error = message
            self.state.install_failed = True
        elif event_type == "install-complete":
            self.state.install_result = payload
            self.state.install_failed = False

        self._refresh_install_step()
        self._sync_summary()

    def _finish_install(self, return_code: int) -> None:
        self.state.install_started = False
        if self._ephemeral_plan_file:
            Path(self._ephemeral_plan_file).unlink(missing_ok=True)
            self._ephemeral_plan_file = None
        if return_code == 0 and self.state.install_result is not None:
            self._go_to_step("complete")
            return
        if not self.state.install_error:
            self.state.install_error = f"Install backend exited with {return_code}"
        self.state.install_failed = True
        self._refresh_install_step()
        self._sync_actions()
        self._sync_summary()
        self.notify(self.state.install_error, severity="error")


def run_app() -> None:
    InstallerApp().run()


if __name__ == "__main__":
    run_app()
