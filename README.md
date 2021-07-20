# GOMON
gomon is a tiny tool that runs a go program and watch changes on it.

gomon was originally a fork of [rld](https://github.com/codehakase/rld)

## Installation
Clone the git repository and build:
```shell
$ git clone https://github.com/yoruba-codigy/gomon
$ cd gomon
$ make release
```

Or install go binary:
```shell
$ cd gomon
$ go install github.com/yoruba-codigy/gomon
```

## Usage
Show help text:
```shell
$ gomon -h
```

`gomon` can watch for changes in file and directories
 
- file:
```shell
$ gomon cmd/main.go
```

- file with positional arguments:
```shell
$ gomon cmd/main.go --arg1 --arg2 1 2 3 4
```

- project:
```shell
$ gomon path/to/project/dir
```

If `gomon` is called without arguments, it assumes the current directory is a project directory.

## Operating Systems
- [x] Linux
- [x] MacOS
- [ ] Windows

## ToDo
Contributions are very much welcome, anyone can create a PR with a fix for any of the following issues:

- [x] Kill Previous Running Processes Before Starting A New One
- [ ] Make Killing Of Previous Process Work In Windows
- [ ] Let Users Specify Which Files To Ignore In Project Mode
- [ ] Watch For Changes In Project Directory (File Creation/Deletion/Rename)
- [ ] Fix Bugs That Stealthily Slipped From Me But Not You

## Author
- [@h4ckit](https://twitter.com/h4ckit)
