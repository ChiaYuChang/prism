# Prism Control Room

The operator UI is a relative-URL Vite application served by unprivileged nginx.
It stores the admin token only in `sessionStorage` and talks to the split public
and admin API listeners through nginx.

Run checks through the repository Task wrappers:

```bash
task web:test
task web:build
task web:dev
```
