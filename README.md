# goqite

<img src="logo.png" alt="Logo" width="300" align="right">

[![GoDoc](https://pkg.go.dev/badge/github.com/maragudk/goqite)](https://pkg.go.dev/github.com/maragudk/goqite)
[![Go](https://github.com/maragudk/goqite/actions/workflows/ci.yml/badge.svg)](https://github.com/maragudk/goqite/actions/workflows/ci.yml)

goqite (pronounced Go-queue-ite) is a persistent message queue Go library built on SQLite and inspired by AWS SQS.

## Features

- Messages are persisted in a SQLite table.
- Messages are sent to and received from the queue, and are guaranteed to not be redelivered before a timeout occurs.

Made in 🇩🇰 by [maragu](https://www.maragu.dk/), maker of [online Go courses](https://www.golang.dk/).
