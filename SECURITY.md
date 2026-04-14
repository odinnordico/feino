# Security Policy

## Supported versions

Only the latest commit on `main` is actively maintained. There are no versioned releases at this time.

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report security issues by emailing **odin.nordico90@gmail.com** with:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept (if safe to share)
- Any suggested mitigations you have in mind

You will receive an acknowledgement within **48 hours** and a more detailed response within **7 days**. If the issue is confirmed we will coordinate a fix and disclosure timeline with you.

## Scope

The following are considered in scope:

- Permission gate bypass (escalating past the configured `permission_level`)
- Path traversal that escapes the `allowed_paths` allowlist
- AST blacklist bypass via shell quoting or command substitution
- Credential leakage (API keys, browser cookies, OS keyring contents)
- Remote code execution via malformed tool arguments or plugin manifests
- Connect RPC endpoint security issues (auth bypass, SSRF via web_fetch/http_*)

The following are **out of scope**:

- Attacks that require the attacker to already have write access to `~/.feino/`
- Denial of service against the local process
- Issues in third-party dependencies (report those upstream)
- Findings from automated scanners without a working proof of concept

## Security design notes

For an overview of the security gate architecture (permission levels, path allowlist, AST blacklist) see the [Security model](README.md#security-model) section in the README.
