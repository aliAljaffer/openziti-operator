# Access report

`manager access` prints one row per Ziti service: who may dial it, who may bind it, which routers may
carry it, and which posture check roles its Dial policies require.

```sh
go run ./cmd/main.go access --connection default
go run ./cmd/main.go access --connection default -o json
```

The manager image has the same subcommand:

```sh
kubectl -n ziti-operator-system exec deploy/ziti-operator -c manager -- \
  /manager access --connection default
```

The report is read-only. It complements [audit](audit.md), which reports broken paths, and
[export](export.md), which produces manifests. Use it for access reviews and for answering "who can
reach this service?" without reading every policy by hand.

`@id` roles are shown as `@name` when the entity is in the network. Posture roles are shown as
`#attribute` or `@name`.
