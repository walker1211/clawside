## ADDED Requirements

### Requirement: Local sender requests require authentication

The system SHALL require a dedicated local sender authentication key for `POST /send`. The authentication key MUST be
distinct from any Telegram bot token and MUST NOT be exposed in logs or status APIs.

#### Scenario: Missing authentication key

- **WHEN** a caller sends `POST /send` without the required authentication header
- **THEN** the system rejects the request with an authentication error
- **AND** the request is not enqueued

#### Scenario: Invalid authentication key

- **WHEN** a caller sends `POST /send` with an incorrect authentication key
- **THEN** the system rejects the request with an authentication error
- **AND** the request is not enqueued

### Requirement: Sender enqueue supports idempotency

The system SHALL support request-level idempotency for `POST /send` using a caller-provided idempotency key. Repeated
requests with the same idempotency key MUST return the existing job instead of creating a duplicate job.

#### Scenario: First request with a new idempotency key

- **WHEN** a caller submits a valid `POST /send` request with an idempotency key that has not been seen before
- **THEN** the system creates a new job
- **AND** returns that job in the response

#### Scenario: Repeated request with the same idempotency key

- **WHEN** a caller repeats the same business request using an idempotency key that already exists
- **THEN** the system does not create a second job
- **AND** returns the existing job information

### Requirement: Sending jobs recover from interrupted delivery attempts

The system SHALL detect interrupted `sending` jobs and recover them according to an explicit lease-based rule so that
they do not remain stuck indefinitely.

#### Scenario: Startup recovers expired sending jobs

- **WHEN** the service starts and finds jobs in `sending` whose delivery lease has expired
- **THEN** the system moves those jobs back into a recoverable state
- **AND** records that delivery was interrupted during sending

#### Scenario: Non-expired sending jobs are preserved

- **WHEN** the service starts and finds jobs in `sending` whose delivery lease has not expired
- **THEN** the system leaves those jobs unchanged

### Requirement: Minimal sender status APIs are available

The system SHALL expose a health endpoint and a per-job status endpoint so callers can verify service health and inspect
delivery state without reading the database directly.

#### Scenario: Health check succeeds

- **WHEN** a caller requests `GET /healthz`
- **THEN** the system returns a successful health response

#### Scenario: Existing job status can be queried

- **WHEN** a caller requests `GET /jobs/{job_id}` for an existing job
- **THEN** the system returns `job_id`, `status`, `attempt_count`, `last_error`, `created_at`, `updated_at`, and
  `sent_at`

#### Scenario: Unknown job id returns not found

- **WHEN** a caller requests `GET /jobs/{job_id}` for a job that does not exist
- **THEN** the system returns a not found response

### Requirement: Sender enforces plain-text input boundaries

The system SHALL accept only supported plain-text requests for the current MVP and SHALL reject overlong text before
enqueue.

#### Scenario: Empty text is rejected

- **WHEN** a caller sends `POST /send` with empty or whitespace-only text
- **THEN** the system rejects the request with a validation error

#### Scenario: Overlong text is rejected

- **WHEN** a caller sends `POST /send` with text longer than the supported Telegram plain-text limit
- **THEN** the system rejects the request with a validation error
- **AND** the request is not enqueued

#### Scenario: Unknown bot is rejected before enqueue

- **WHEN** a caller sends `POST /send` with a bot name that is not in the configured bot allowlist
- **THEN** the system rejects the request with a validation error
- **AND** the request is not enqueued
