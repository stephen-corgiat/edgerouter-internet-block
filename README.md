# edgerouter-internet-block

Small Go HTTP service that runs on a Ubiquiti EdgeRouter X (EdgeOS) and toggles
network access for LAN devices by mutating kernel `ipset`s. Each `ipset` is
bound to a static EdgeOS firewall rule, so the service never touches iptables
directly.

The service exposes per-**block-type** ipsets. Each type maps to its own ipset
(and its own firewall rule on the router). The type name is arbitrary —
whatever category of traffic you want to manage (e.g. `internet`, `games`,
`social`) — and is just the `{type}` segment in the URL. The client supplies
the LAN IPs to toggle on each request via the `?ip=` query parameter
(comma-separated).

`POST /internet/add?ip=192.168.1.50,192.168.1.51` adds those IPs to the
`internet` ipset, leaving every other type untouched.

## Build

Requires Go 1.22+.

```bash
# Native (for tests / dev)
go build ./...
go vet ./...
go test ./...

# Cross-compile for EdgeRouter X (MT7621, MIPS little-endian, soft-float)
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -o ./internet-block .
```

## One-time router configuration

Run on the router via `configure`. The example below sets up the `internet`
type — repeat the address-group + WAN_OUT rule pair for any other type you
add to `ipset_names`.

```bash
configure

# 1. Address group — one per block type. The address-group name MUST match the
#    value in config.json -> ipset_names[<type>]. A placeholder address (127.0.0.1)
#    is required so EdgeOS will create the kernel ipset before any IP is added.
set firewall group address-group blocked-internet description "Dynamically managed: Blocks Internet"
set firewall group address-group blocked-internet address 127.0.0.1

# 2. WAN_OUT firewall rule. Rule numbers are arbitrary as long as they don't
#    collide with existing rules; lower numbers are evaluated first.
set firewall name WAN_OUT rule 2 action drop
set firewall name WAN_OUT rule 2 description "Blocks Internet for IPs in dynamically managed list"
set firewall name WAN_OUT rule 2 protocol all
set firewall name WAN_OUT rule 2 source group address-group blocked-internet

commit
save
exit
```

For a category-specific type (e.g. blocking only certain services), add a
`destination group address-group <your-destinations>` clause to the rule and
maintain that destination group separately — this service only manages the
*source* (per-device) ipset.

Verify the kernel ipset and the iptables reference:

```bash
sudo ipset list blocked-internet
sudo iptables -L WAN_OUT -n | grep blocked-
```

## Deploy

```bash
ssh ubnt@192.168.1.1 'mkdir -p /config/internet-block /config/scripts/post-config.d'

scp internet-block                          ubnt@192.168.1.1:/config/internet-block/
scp deploy/config.example.json              ubnt@192.168.1.1:/config/internet-block/config.json   # then edit access_key
scp deploy/internet-block.service           ubnt@192.168.1.1:/config/internet-block/
scp deploy/post-config.d-internet-block.sh  ubnt@192.168.1.1:/config/scripts/post-config.d/internet-block.sh

ssh ubnt@192.168.1.1
chmod +x /config/internet-block/internet-block /config/scripts/post-config.d/internet-block.sh
sudo /config/scripts/post-config.d/internet-block.sh
sudo systemctl status internet-block
```

`/config` survives firmware upgrades; `/etc` does not. The post-config.d hook
re-installs the systemd unit on every boot.

## Configuration

`/config/internet-block/config.json`:

| Field         | Default            | Notes                                     |
| ------------- | ------------------ | ----------------------------------------- |
| `listen_addr` | `192.168.1.1`      | Bind to LAN IP only — service is unreachable from WAN by construction. |
| `port`        | `8080`             |                                           |
| `access_key`  | _(required)_       | Shared secret for `Authorization: Bearer`. |
| `ipset_names` | _(required, ≥ 1)_  | Map of `type` → EdgeOS address-group name. The `type` keys are the `{type}` segment in the URL. |

Example:

```json
{
  "access_key": "...",
  "ipset_names": {
    "internet": "blocked-internet"
  }
}
```


## API

All routes require `Authorization: Bearer <access_key>`.

| Method | Path                  | Required query             | Description                                                          |
| ------ | --------------------- | -------------------------- | -------------------------------------------------------------------- |
| POST   | `/{type}/add`         | `ip=<ip>[,<ip>...]`        | `ipset add` every IP in the list to `ipset_names[type]`.             |
| POST   | `/{type}/del`         | `ip=<ip>[,<ip>...]`        | `ipset del` every IP in the list from `ipset_names[type]`.           |
| GET    | `/{type}/status`      | `ip=<ip>[,<ip>...]`        | Per-IP `present` flag (true if the IP is in the ipset) plus `all_present` (true when **every** requested IP is in the set) and raw ipset members. |

Each entry in `ip` is parsed with `net.ParseIP` and rejected if invalid — the
service never passes unvalidated input to the `ipset` binary.

Errors:

- Unknown `{type}` → `404 unknown type`.
- Missing `ip` → `400 ip query parameter required`.
- Invalid IP in list → `400 invalid ip: <value>`.

`/{type}/status` response — `present` is true when the IP is a member of the
ipset; `all_present` is true when every requested IP is:

```json
{
  "type": "internet",
  "all_present": false,
  "devices": [
    {"ip": "192.168.1.50", "present": true},
    {"ip": "192.168.1.51", "present": false}
  ],
  "members": ["127.0.0.1", "192.168.1.50"]
}
```

`/{type}/add` and `/{type}/del` response:

```json
{
  "type": "internet",
  "members": ["127.0.0.1", "192.168.1.50", "192.168.1.51"]
}
```

## Quick smoke test

```bash
KEY=...
curl -s -H "Authorization: Bearer $KEY" "http://192.168.1.1:8080/internet/status?ip=192.168.1.50,192.168.1.51" | jq
curl -s -X POST -H "Authorization: Bearer $KEY" "http://192.168.1.1:8080/internet/add?ip=192.168.1.50,192.168.1.51" | jq
curl -s -X POST -H "Authorization: Bearer $KEY" "http://192.168.1.1:8080/internet/del?ip=192.168.1.60" | jq
```

## Notes

- Blocked state is in-memory (kernel ipset) and intentionally resets on reboot.
- HTTPS is omitted: the bearer key travels in plain text, so only use this on a
  trusted LAN segment and keep `listen_addr` bound to the LAN interface. Use a
  long random `access_key`.
- Running as `root` because `ipset` requires `CAP_NET_ADMIN`.
- The service only manages the *source* (per-device) ipsets. For category-based
  rules, the destination ipset (the list of provider IPs) is maintained
  separately on the router.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
