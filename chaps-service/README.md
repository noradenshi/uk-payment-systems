# CHAPS Service

## Summary of the API Layout

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/chaps` | Initiate a new high-value CHAPS payment. Accepts ISO 20022 XML or GUI JSON payloads. | Done |
| **POST** | `/v1/payments/chaps/{id}/authorize` | Second-factor approval required for high-value release. | Done |
| **GET** | `/v1/payments/chaps/{id}` | Retrieve the real-time settlement status, ISO 20022 details, and audit trail of a specific payment. | Done |
| **GET** | `/v1/payments/chaps` | List and filter historical CHAPS payments by status and limit. | Done |
| **POST** | `/v1/payments/chaps/validate` | Perform dry-run validation: BIC formatting, participant status, receiver existence, and liquidity availability. | Done |
| **DELETE** | `/v1/payments/chaps/{id}` | Attempt to cancel a payment. Only available if the status is `PENDING`. | Done |
| **POST** | `/v1/payments/chaps/{id}/amend` | Amend non-financial details of a pending payment. | Done |
| **GET** | `/v1/payments/chaps/limits` | Retrieve current clearing limits and remaining intraday liquidity. | Done |
| **GET** | `/v1/participants` | List participants for the operator GUI. | Done |
| **POST** | `/v1/participants/register` | Initial onboarding of a bank's BIC and settlement account. | Done |
| **PATCH** | `/v1/participants/{bic}/status` | Updates visibility, for example `ACTIVE`, `SUSPENDED`, or `DISABLED`. | Done |
| **POST** | `/v1/participants/{bic}/block` | Immediate kill-switch. Halts all outbound settlement instructions. | Done |
| **GET** | `/v1/participants/{bic}/block` | Returns details about block status, time, reason, and operator. | Done |
| **DELETE** | `/v1/participants/{bic}/block` | Unblocks the participant and restores settlement rights. | Done |
| **GET** | `/v1/participants/{bic}/positions` | Real-time view of `Earmarked` vs. `Available` liquidity. | Done |
| **GET** | `/v1/system/schedule` | Returns today's cut-off times. | Done |
| **POST** | `/v1/liquidity/top-up` | Simulation only: injects funds into the bank's settlement account. | Done |

## GUI

The React GUI in `web/chaps-gui` is wired to the `/v1/...` backend through the Vite proxy. It includes controls for payment creation, validation, authorization, amend/cancel, participant onboarding, status changes, block/unblock, liquidity top-up, position lookup, limits, schedule, participants, and payment history.
