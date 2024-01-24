# goqite

<img src="logo.png" alt="Logo" width="300" align="right">

[![GoDoc](https://pkg.go.dev/badge/github.com/maragudk/goqite)](https://pkg.go.dev/github.com/maragudk/goqite)
[![Go](https://github.com/maragudk/goqite/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/goqite/actions/workflows/ci.yml)

goqite (pronounced Go-queue-ite) is a persistent message queue Go library built on SQLite and inspired by AWS SQS.

## Features

- Messages are persisted in a SQLite table.
- Messages are sent to and received from the queue, and are guaranteed to not be redelivered before a timeout occurs.

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
