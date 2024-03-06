# goqite

<img src="docs/logo.png" alt="Logo" width="300" align="right">

[![GoDoc](https://pkg.go.dev/badge/github.com/maragudk/goqite)](https://pkg.go.dev/github.com/maragudk/goqite)
[![Go](https://github.com/maragudk/goqite/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/goqite/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/maragudk/goqite/graph/badge.svg?token=DxGkk2lLHF)](https://codecov.io/gh/maragudk/goqite)

goqite (pronounced Go-queue-ite) is a persistent message queue Go library built on SQLite and inspired by AWS SQS (but much simpler).

## Features

- Messages are persisted in a SQLite table.
- Messages are sent to and received from the queue, and are guaranteed to not be redelivered before a timeout occurs.
- Support for multiple queues in one table.
- Message timeouts can be extended, to support e.g. long-running tasks.
- A simple HTTP handler is provided for your convenience.
- No non-test dependencies. Bring your own SQLite driver.

## Benchmarks

Just for fun, some benchmarks. 🤓

On a MacBook Air with M1 chip and SSD, sequentially sending, receiving, and deleting a message:

```shell
$ make benchmark
go test -cpu 1,2,4,8 -bench=.
goos: darwin
goarch: arm64
pkg: github.com/maragudk/goqite
BenchmarkQueue/send_and_receive_messages           	   15885	     76166 ns/op
BenchmarkQueue/send_and_receive_messages-2         	   17215	     66464 ns/op
BenchmarkQueue/send_and_receive_messages-4         	   18106	     63733 ns/op
BenchmarkQueue/send_and_receive_messages-8         	   19184	     62530 ns/op
PASS
ok  	github.com/maragudk/goqite	7.895s
```

Note that the slowest result above is around 13,000 messages / second.

Made in 🇩🇰 by [maragu](https://www.maragu.dk/), maker of [online Go courses](https://www.golang.dk/).
