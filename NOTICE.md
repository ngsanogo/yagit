# yagit — NOTICE

yagit is free software licensed under the **MIT License**, an
[OSI-approved](https://opensource.org/licenses) license.

## Policy

- The yagit product line does **not** use paid feature locks.
- Features are either available in the open source tree or tracked publicly in
  `docs/ROADMAP.md` as work to implement — not as commercial entitlements.
- Contributions are accepted only under MIT (same terms as the project).
- Do not import code that is not under an OSI-approved license compatible with
  this project’s MIT distribution.
- Prefer **100% OSI open-source** for dependencies and the official stack; avoid
  non-OSI “source available” licenses and proprietary components that create
  avoidable legal risk. Full rules: [`OPEN_SOURCE_POLICY.md`](OPEN_SOURCE_POLICY.md).

## Direct dependencies (Go)

| Package | License |
| ------- | ------- |
| `github.com/fsnotify/fsnotify` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |

Frontend dependencies are declared in `web/package.json` with their lockfile;
each must remain OSI-approved under [`OPEN_SOURCE_POLICY.md`](OPEN_SOURCE_POLICY.md).

## Copyright

Copyright notices appearing in source files remain those of their respective
authors and contributors. Preserve them when you modify or redistribute the
software, as required by the MIT License.
