# Agent configuration

Everything an AI coding agent needs to work in this repository lives here.
The repository publishes two links to it, and nothing else:

- `AGENTS.md` at the repository root points at [`AGENTS.md`](AGENTS.md)
- `.agents/skills` points at [`skills/`](skills/)

Do not edit the links by hand. Run `./do agent sync` if one is missing, and
`./do agent check` to verify them. CI runs the check as part of `./do lint`.

## Layout

```
agent/
  AGENTS.md         entry point, linked as AGENTS.md at the repository root
  rules/            modular instructions, one concern per file
  skills/           workflow skills (SKILL.md per directory)
```

`AGENTS.md` says which rule file applies to which part of the tree. Skills
follow the [Agent Skills](https://agentskills.io) layout: a directory with a
`SKILL.md` file and optional supporting files. `.agents/skills` is where an
agent looks for them.

## Contributing

1. Edit files under `agent/` only.
2. Run `./do agent sync` when a link is missing, or when this checkout had to
   copy the targets instead of linking them.
3. Run `./do agent check` — CI runs this in the lint gate.

On a checkout that can create symlinks, editing a file under `agent/` is
visible immediately: the links follow the files. On a checkout that copied
them, `./do agent check` fails until sync refreshes the copies.

## Paths an agent does not read

[`.ignore`](../.ignore) lists them, in the same syntax as `.gitignore`. It
covers this machine's `.env` and `.yagit/`, the build output `./do build`
reproduces, and `web/pnpm-lock.yaml`, which is tracked and generated.
Respect the list even when a search tool does not.

## Personal files

Notes that belong to one checkout match `*.local.md`. That pattern is in
`.gitignore` and in `.ignore`. Such a file is not a way to configure the
project.
