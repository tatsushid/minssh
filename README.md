MinSSH
======

MinSSH is a SSH client with minimum functions written in Go. It can run on
Linux, macOS and __Windows__.

## Features

- Support command, subsystem and interactive shell mode
- Work on Windows Command Prompt and PowerShell (Especially work well on
  Windows 10 AU or later)
- Can read OpenSSH `known_hosts` file and verify host
- Support OpenSSH public key, keyboard interactive and password authentication

## Install

Download your platform binary from
[Release page](https://github.com/tatsushid/minssh/releases) and put it
somewhere you like.

If you'd like to build it by yourself, please use `go get`:

```shellsession
$ go get -u github.com/tatsushid/minssh
```

## Usage

Run the command like

```shellsession
$ minssh user@hostname
```

You can see command options by

```shellsession
$ minssh -help
```

If you run this on MSYS2/Cygwin with Mintty, please wrap this by
[winpty](https://github.com/rprichard/winpty) like

```shellsession
$ winpty minssh user@hostname
```

It saves its own data in

- `$HOME/.minssh/` (Linux, macOS)
- `%APPDATA%\minssh\` (Windows)

## Contribution

1. Fork ([https://github.com/tatsushid/minssh/fork](https://github.com/tatsushid/minssh/fork))
2. Create a feature branch
3. Commit your changes
4. Rebase your local changes against the master branch
5. Run test suite with the `go test ./...` command and confirm that it passes
6. Run `go fmt`
7. Create new Pull Request

## License

MinSSH is under MIT license. See the [LICENSE](./LICENSE) file for details.
