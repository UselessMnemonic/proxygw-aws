# AWS Plugin

`proxygw-aws` is an external plugin module that provides AWS-backed target kinds.

## Plugin Setup

Add the plugin to the `proxygw` daemon build in the main `proxygw` repository:

1. Add this module as a dependency:

```sh
go get github.com/UselessMnemonic/proxygw-aws@latest
```

2. Register the plugin in `plugin.yaml`:

```yaml
plugins:
  github.com/UselessMnemonic/proxygw-aws: aws
```

3. Regenerate the plugin import file and rebuild the daemon:

```sh
go generate ./cmd/proxygw
make proxygw
```

The plugin registers under the module path
`github.com/UselessMnemonic/proxygw-aws` and uses the `aws` namespace, so
target kinds are referenced as `aws:...`.

## Exported Kinds

Targets:

- `ec2`: starts EC2 instances when the target warms and stops them when the
  target drains. Stopping can optionally request EC2 hibernation.

There is no plugin-level configuration for this plugin. AWS credentials and defaults
come from the AWS SDK for Go v2 default credential chain.

## ec2 Target

Example:

```yaml
targets:
  - name: backend
    kind: aws:ec2
    idle_timeout: 10m
    endpoints:
      - name: http
        protocol: tcp
        address: 10.0.1.25:8080
    options:
      region: us-east-1
      instance_id: i-0123456789abcdef0
      hibernate: true
      start_timeout: 10m
      stop_timeout: 10m
```

Options:

- `instance_id`: EC2 instance ID to start and stop.
- `instance_ids`: array of EC2 instance IDs; use instead of `instance_id`.
- `region`: optional AWS region. If omitted, the SDK default region resolution
  is used.
- `profile`: optional shared config profile.
- `hibernate`: optional boolean; defaults to `false`. When true, drain uses EC2
  `StopInstances` with hibernation requested.
- `start_timeout`: optional Go duration string; defaults to `10m`.
- `stop_timeout`: optional Go duration string; defaults to `10m`.
