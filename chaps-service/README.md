### Summary of the API Layout
| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/chaps` | Initiate a new high-value CHAPS payment. Requires idempotency key and digital signature. | 🚧 |
| **POST** | `/v1/payments/chaps/{id}/authorize` | Second-factor approval required for high-value release. | ❌ |
| **GET** | `/v1/payments/chaps/{id}` | Retrieve the real-time settlement status, ISO 20022 details, and audit trail of a specific payment. | ✔️ |
| **GET** | `/v1/payments/chaps` | List and filter historical CHAPS payments by date range, status, or amount. | ❌ |
| **POST** | `/v1/payments/chaps/validate` | Perform dry-run validation: checks BIC/IBAN formatting, sanction screening, and liquidity availability. | ❌ |
| **DELETE** | `/v1/payments/chaps/{id}` | Attempt to cancel a payment. Only available if the status is PENDING and it hasn't reached RTGS. | ❌ |
| **POST** | `/v1/payments/chaps/{id}/amend` | Amend non-financial details (e.g., remittance info) of a pending payment. | ❌ |
| **GET** | `/v1/payments/chaps/limits` | Retrieve current clearing limits and remaining intraday liquidity. | 🚧 |
| **POST** | `/v1/participants/register` | Initial onboarding of a bank's BIC and settlement account. | 🚧 |
| **PATCH** | `/v1/participants/{bic}/status` | Updates visibility (e.g., `ACTIVE`, `SUSPENDED`, `DISABLED`). | ❌ |
| **POST** | `/v1/participants/{bic}/block` | Immediate "Kill-switch." Halts all outbound settlement instructions. | 🚧 |
| **GET** | `/v1/participants/{bic}/block` | Returns details: who blocked the bank, when, and the reason (e.g., `FRAUD_SUSPECTED`). | 🚧 |
| **DELETE** | `/v1/participants/{bic}/block` | Unblocks the participant and restores settlement rights. | ✔️ |
| **GET** | `/v1/participants/{bic}/positions` | Real-time view of `Earmarked` vs. `Available` liquidity. |🚧 |
| **GET** | `/v1/system/schedule` | Returns today's cut-off times (which can change on bank holidays). | 🚧 |
| **POST** | `/v1/liquidity/top-up` | **Simulation Only:** Injects funds into the bank's settlement account. | 🚧 |
