# Starting a node automatically on Linux

**English** · [Deutsch](de/README.md)

For computers on which a node should run permanently (VM, server, workstation), without root: as a **systemd user service** of the user who owns `~/.swarmainet/`. For a dedicated system user, as with the helper on the Raspberry Pi, see [`deploy/raspberry-pi/`](../raspberry-pi/README.md).

```sh
# Build the program and copy it to a fixed place (that way a later go build
# in the project does not accidentally change the running service)
go build -o swarmainet ./cmd/swarmainet
install -D -m 0755 swarmainet ~/.local/bin/swarmainet

# Set up the service
install -D -m 0644 deploy/linux/swarmainet.service ~/.config/systemd/user/swarmainet.service
systemctl --user daemon-reload

# Start at boot even without logging in
loginctl enable-linger "$USER"

# First stop a node running in the terminal (Ctrl+C), then:
systemctl --user enable --now swarmainet
```

Operating it:

```sh
systemctl --user status swarmainet
journalctl --user -u swarmainet -f        # log
systemctl --user restart swarmainet       # after changes to the configuration
swarmainet status                          # as usual
```

**Updating:** rebuild, copy to `~/.local/bin/swarmainet`, `systemctl --user restart swarmainet`.

## Setting up a node for a group network

On a fresh computer, `gruppe-beitreten.sh` does everything after `git clone`, including installing Go, and runs without root. Tested with Ubuntu 24.04; `git`, `python3` and `systemd` are required. The script's messages and prompts are in German.

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
deploy/linux/gruppe-beitreten.sh
```

The script downloads the Go version from `go.mod` to `~/.local/go` and verifies the checksum, builds the program, creates `~/gruppe-test`, asks for the invitation link and starts the node. An existing SwarmAiNet node stays untouched, because directory, port (4714) and gateway (7715) are separate. Running it again updates the program and restarts the node without joining again.

The issuer creates the link: `swarmainet member invite -net m-… -peer <address of the helper>`.

```sh
deploy/linux/gruppe-beitreten.sh status      # connections
deploy/linux/gruppe-beitreten.sh test        # test task
deploy/linux/gruppe-beitreten.sh stop        # stop
deploy/linux/gruppe-beitreten.sh entfernen   # delete node and directory
```

The node runs as a transient service and does not survive a reboot of the computer; then run the script again. For continuous operation, follow the guide above. The same unit template also works for a further node, e.g. the issuer of a group network: copy it under another name (e.g. `swarmainet-gruppe.service`) and adjust `ExecStart` to its program and configuration.
