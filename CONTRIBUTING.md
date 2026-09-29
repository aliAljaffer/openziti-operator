# Contributing

## Sign your commits

This project uses the [Developer Certificate of Origin](https://developercertificate.org/). Add a sign-off to every commit:

```sh
git commit -s
```

## Run the tests

```sh
go test ./...
```

Integration tests need a Ziti controller:

```sh
ZITI_MGMT_URL=https://localhost:1280/edge/management/v1 \
ZITI_USERNAME=admin ZITI_PASSWORD=admin123 \
go test -tags integration ./internal/ziti/
```

To start a local controller:

```sh
docker run -d --name ziti -p 1280:1280 openziti/ziti-cli:2.0.4 \
  edge quickstart --ctrl-address localhost --ctrl-port 1280 --password admin123
```

## Rules

- Do not add organization names, hostnames, IDs, or addresses to code, tests, or docs.
- The operator changes only entities that carry its `ziti-operator-uid` tag.
