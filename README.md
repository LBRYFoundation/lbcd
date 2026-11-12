lbcd
====

[![Build Status](https://github.com/LBRYFoundation/lbcd/workflows/Build%20and%20Test/badge.svg)](https://github.com/LBRYFoundation/lbcd/actions)
[![Coverage Status](https://coveralls.io/repos/github/LBRYFoundation/lbcd/badge.svg?branch=master)](https://coveralls.io/github/LBRYFoundation/lbcd?branch=master)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)


**lbcd** is a full node implementation of the LBRY blockchain written in Go
(golang). It downloads, validates and serves the block chain, relays blocks and
transactions, and maintains a transaction pool.

Note: **lbcd** does *NOT* include wallet functionality. That functionality is
provided by [lbcwallet](https://github.com/LBRYFoundation/lbcwallet) and the
[LBRY SDK](https://github.com/LBRYFoundation/lbry-sdk).

## Requirements

[Go](http://golang.org) 1.25 or newer. All common operating systems are
supported. A node needs a fair amount of RAM and disk space, both of which grow
slowly over time; a fast NVMe disk is recommended.

## Installation

Binary releases are published at
https://github.com/LBRYFoundation/lbcd/releases

#### Build from Source (Linux/BSD/macOS/POSIX)

- Install Go according to its [installation instructions](http://golang.org/doc/install)
  and check that it works:

```bash
$ go version
```

- Clone and build:

```bash
$ git clone https://github.com/LBRYFoundation/lbcd
$ cd lbcd
$ go install -v . ./cmd/...
```

- The node and its utilities are installed in `$(go env GOPATH)/bin`. Add that
  directory to your `PATH` if it isn't already there.

## Updating

```bash
$ cd lbcd
$ git pull
$ go install -v . ./cmd/...
```

## Getting Started

lbcd has several configuration options to tweak how it runs, but it works with
zero configuration. See `sample-btcd.conf` for the available options.

```bash
$ btcd
```

## Issue Tracker

Please report problems and request features using the
[GitHub issue tracker](https://github.com/LBRYFoundation/lbcd/issues).

## Documentation

The documentation is a work-in-progress. It is located in the
[docs](docs) folder.

## Release Verification

See the [release documentation](release) for how to verify the integrity of
published releases using the reproducible build system.

## License

lbcd is licensed under the [copyfree](http://copyfree.org) ISC License.
