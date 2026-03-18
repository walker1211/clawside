## 1. Configuration and API contract

- [ ] 1.1 Extend runtime config to load a dedicated sender authentication key from `config.toml`
- [ ] 1.2 Update config builder and related tests/docs so generated config includes the sender authentication key
  contract
- [ ] 1.3 Extend `POST /send` request/response contract to include idempotency key support

## 2. HTTP handler hardening

- [ ] 2.1 Add failing tests for missing and invalid `/send` authentication
- [ ] 2.2 Implement `/send` authentication without logging the sender key
- [ ] 2.3 Add failing tests for idempotency key reuse returning the existing job
- [ ] 2.4 Add failing tests for overlong text rejection before enqueue
- [ ] 2.5 Implement idempotent enqueue path and text length validation in the HTTP handler

## 3. Store and recovery model

- [ ] 3.1 Add schema migration for idempotency and lease fields on jobs
- [ ] 3.2 Add store tests for idempotent lookup/insert behavior
- [ ] 3.3 Add store and worker tests for lease-based recovery of interrupted `sending` jobs
- [ ] 3.4 Implement lease expiry tracking when a worker claims a job
- [ ] 3.5 Update startup recovery to move only expired `sending` jobs into a recoverable retry state

## 4. Status APIs

- [ ] 4.1 Add failing tests for `GET /healthz`
- [ ] 4.2 Add failing tests for `GET /jobs/{job_id}` success and not-found cases
- [ ] 4.3 Implement `GET /healthz` and `GET /jobs/{job_id}` responses with the required job fields

## 5. Verification and documentation

- [ ] 5.1 Update README for sender key, idempotency key, status APIs, and text length boundary
- [ ] 5.2 Run focused Go tests for handler, store, worker, and config changes
- [ ] 5.3 Run full verification (`go test ./...`, `go build ./...`, Python config builder tests)
