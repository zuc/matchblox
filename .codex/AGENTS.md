# matchblox — Codex notes

`testdata/*/proc/*/cwd` are fake `/proc` cwd symlinks. They must not point at
`/` (or any real tree): tools that walk with `--follow` hang on the disk.
They use the dangling target `/matchblox-fixture-root`. Leave them alone
unless you are changing those fixtures; `os.Readlink` is what the tests need.
