# Architecture notes

About this implementation, not about ledgers in general. File and function
names refer to `internal/ledger` unless stated.

## 1. Append-only at scale

### What breaks first at 100x volume

End of day, and it breaks before anything else gets slow.

`CloseDay` re-decides every day since Day 1 for every account, every night:

- `reassessFees` walks days `1..today` and calls `store.Entries(account)` once
  per day per pass (two passes: one that works, one that confirms).
- `MemStore.Entries` is not indexed. It scans the entries of all accounts
  under the store mutex and copies the matches.
- `accrue` then sums the account's entries once more per day to get each
  day's basis.

So one close costs roughly `days so far x accounts x all entries`, and "all
entries" itself grows with days and accounts. The work per night grows every
night even if the business does not. `CloseDay` also holds the ledger mutex
for its whole run, so nothing can be posted or authorized until it finishes.

Next in line, in order:

1. Every balance read is a full scan and copy: `Balance`, `Available`, and
   therefore every `Authorize`. Authorization latency grows with total
   history.
2. One store mutex and one sequence counter: a single writer at a time,
   across all accounts.

### Where state grows without bound

| State | Why it grows | Inherent to append-only? |
|---|---|---|
| The four logs (entries, auth decisions, accruals, rejections) | Nothing is ever removed | Yes, that is the design |
| The restatement horizon | Any entry may carry any value day back to Day 1, so every closed day stays open to change for ever and must be re-examined every night | No. This is the one that hurts |
| Accrual lines | One per account per day even at zero balance (ACC-002 has four lines of 0.000), plus one adjustment per restated day per restatement | Partly |
| Active holds | No expiry: an approved, unsettled hold is counted in every `Available` call for ever | No |
| Notices | A negative day in a currency with no fee is reported again at every close until cured | No |

### The cheapest structural change that defers it

An earliest-affected-day watermark in end of day.

Nothing booked today can change a day earlier than the smallest value date
among today's entries for that account. Fees and waivers appended by the run
are value-dated on or after that day too. So `reassessFees` and `accrue` can
start from that day instead of Day 1, and an account with no backdated entry
today re-examines only today.

The watermark is derived from entries where `Booked == today`. It needs no new
stored state, no change to the `Store` interface, and about fifteen lines.
Per-night cost goes from "all days" to "days actually touched".

It defers the problem; it does not bound it. One entry value-dated Day 1
still re-walks everything. What comes after, in order of cost:

1. A per-account index inside the store, so a read does not scan other
   accounts. Trivial, and invisible to the ledger.
2. A back-value limit plus per-account checkpoints: an appended record saying
   "balance of value day X as known at sequence S", superseded by later ones,
   never updated. This is where "balances are never stored" has to become
   "a stored balance is never the source of truth", and where it must be
   decided what happens to an entry dated before the limit.

## 2. Value-dated entries in production

### Operational surface

What value dating does in this code that a bank would have to own:

- **Reported figures change after the fact.** The replay's `Earlier days
  restated` section is exactly this. Statements, balance confirmations,
  general-ledger postings and regulatory returns produced from the earlier
  figure are now out of date. The code has no notion of "already reported",
  so it cannot tell a harmless restatement from one that contradicts a
  document a customer holds.
- **Retroactive charges are automatic.** One backdated debit made the next
  close charge three fees, two of them for days the customer had seen as
  positive. No person reviewed it.
- **Retroactive interest goes both ways.** A negative adjustment line takes
  back interest that was already accrued.
- **Decisions were made on balances that were later restated.** An
  authorization approved or declined on the old figure stays as decided.
- **Back-value dating is a classic fraud and error route**: date a credit
  back to earn interest or erase a fee. Here any caller may pass any value
  day from 1 upward, with no limit, no approval, no reason code, and no
  record of who.
- **There is no closed period.** Nothing stops a value date in a period whose
  books have been closed.

### Regulatory surface in a UAE-licensed bank

I searched for each item below. The status column says how far it was
confirmed. I could not open the primary texts: the rulebook site refused the
fetch, and the Standards PDF downloaded but could not be converted to text in
this environment. So nothing below is quoted from a document I read end to
end, and I cite no article number that I did not see in a search result.

| Topic | What I found | Status |
|---|---|---|
| Conduct towards retail customers | The CBUAE has a Consumer Protection Regulation and accompanying Consumer Protection Standards. The Standards state they are "mandatory and enforceable in the same manner as the Regulation". They require fees to be disclosed under "Article 2: Disclosure and Transparency", refer to a "Maximum Fees Annexure", and deal with complaints under "Article 8: Complaint Management and Complaint Resolution". [1][2][3] | Seen in search results quoting the rulebook. Primary text not opened. The circular number of the Regulation is not confirmed, so not given. |
| Whether an overdraft fee like this one is permitted or capped | Not found. | **Unverified.** The Maximum Fees Annexure is the place to check. |
| Notice before changing fees | A secondary source says 60 calendar days' written notice. | **Unverified** against the Standards. |
| Complaints | Search results quoting the Standards: written acknowledgement within 2 complete business days; complaint records kept for 5 years. [3] | Seen in search results. Clause numbers not confirmed. |
| Record retention | CBUAE AML/CFT guidance: records kept at least five years from completion of the transaction or end of the relationship. [4] | Seen in a search result quoting the rulebook. |
| The AML law behind that guidance | Law firms report that Federal Decree-Law No. 10 of 2025 replaced Federal Decree-Law No. 20 of 2018, in force 14 October 2025. One source says it repeals only certain provisions. [5] | Secondary sources, and they disagree on scope. Article numbers I found belong to the older texts, so none is cited. |
| The banking law itself | Law firms report that Federal Decree-Law No. 6 of 2025 repealed and replaced Federal Decree-Law No. 14 of 2018, effective 16 September 2025, with a one-year transition. [6][7] | Secondary sources, consistent with each other. Text not read. |
| Card and payment obligations | The CBUAE Retail Payment Services and Card Schemes Regulation is listed in the rulebook as C 15/2021. A law-firm summary says it covers liability and refunds for unauthorised payment transactions. [8][9] | Existence confirmed. Whether and how it binds a bank's own account ledger is **unverified**. Sources disagree on its effective date. |
| VAT on fees | Commentary and a bank's own customer notice say financial services charged as an explicit fee are standard-rated at 5%, while margin-based income such as interest is exempt. [10][11] | Secondary sources. Whether an overdraft fee of this kind is in scope is **unverified**; one source treats penalty-type charges differently. |
| A rule specifically about back-value dating customer accounts | Searched; found none. | **Unverified** that none exists. |

What this means for the code, whatever the exact clauses turn out to be:

- A fee charged retroactively is still a fee charged to a consumer, so it
  falls under whatever the Standards say about disclosure and permitted fees.
  The code charges it with no check against a disclosed tariff.
- If the fee is subject to VAT, the `FEE` entry needs a tax component and the
  `FEE_WAIVER` needs a matching credit note. The model has one amount per
  entry and no tax.
- Five-year retention is met trivially by never deleting, and nothing else
  about retention (archiving, retrieval, deletion after the period) exists.
- A complaint about a retroactive fee has to be answered from the record. The
  bitemporal query can reconstruct what was known on any day, which is the
  part of this design that helps.

### One control to add before go-live

A back-value gate with a preview.

Any entry whose value day is earlier than its booked day is not applied
directly. It goes to a pending queue with a reason code and the identity of
the maker, and a second person approves or rejects it. Attached to the
request is the restatement it would cause: the days whose closing balance
changes, the fees that would be charged or waived, and the interest
adjustments. Fees that would be charged to the customer retroactively need
their own explicit approval.

The preview is cheap here because the hard part exists: run the same
end-of-day logic against the store plus the proposed entry, without
appending. It is the same computation that produces `Earlier days restated`.

## 3. Authorization lifecycle

Every way an authorization can end in this model other than a settlement
that matches it, and what the system does. "Should do" is what a production
issuer would need; card-scheme rules were not researched, so that column is
general practice and **unverified**.

| How it ends | Real-world scenario | What this code does | What production should do |
|---|---|---|---|
| **Declined at request.** Available balance would go negative. | Insufficient funds at the till. This is what happens to Auth-B. | Appends an `AuthEvent` with kind `DECLINED` and the reason. No hold, no entry. Not an error. A later settlement naming it is refused (`ErrAuthorizationNotActive`). | The same, plus a response code to the acquirer. A settlement for a declined authorization should go to an exception queue, since a merchant can still present it. |
| **Refused before any decision.** Duplicate id, unknown account, wrong currency, non-positive amount, or a booked day already closed. | A replayed or malformed message. | Appends a `Rejection`, returns an error. No auth event, so the id (unless it was a duplicate) remains unused. | Reject with a reason. A duplicate should return the original decision, not an error; that needs idempotency keys, which do not exist here. |
| **Approved, never settled.** | Abandoned basket; hotel or car-hire pre-authorization never completed; the merchant simply never presents. This is Auth-B in the stream without E7. | Nothing. The hold stays active and is subtracted from available balance for ever. There is no expiry and no void. | Expire the hold after a scheme-defined period by appending an expiry event at end of day, and release it earlier on a merchant void. Not built. |
| **Settled for less than the hold.** Same id, lower amount. | Fuel pump pre-authorization, final bill below the estimate. This is Auth-A. | Accepts. One `SETTLEMENT` entry debits the amount and releases the whole hold, so the difference is spendable at once. Status `SETTLED`. | Depends on the transaction type: either release the remainder, as here, or keep it held for further partial captures. |
| **Settlement for more than the hold.** | A tip added after authorization; currency movement between authorization and clearing. | Refuses (`ErrSettlementExceedsHold`), records a `Rejection`. The authorization stays `APPROVED` with its hold active, so unless a valid settlement follows it becomes a never-settled hold. | Accept within a tolerance set per merchant category, or post and dispute. Refusing outright is not usually open to an issuer. |
| **Second settlement of the same authorization.** | Duplicate presentment. | Refuses (`ErrAuthorizationNotActive`). The first settlement stands. | The same, plus a dispute back to the acquirer. |
| **Settlement that names no authorization on the account.** The authorization never existed, or exists on another account. | Offline or below-floor-limit transaction, force-post, or clearing that arrives after the authorization aged out. | Refuses (`ErrUnknownAuthorization`), records a `Rejection`, moves no funds. | Post to a suspense or exception account and investigate. See AMBIGUITIES.md D1. |

Two endings that a real lifecycle has and this model cannot represent at all:

- **Merchant void.** There is no call that releases a hold without a debit.
- **Refund after settlement.** A `SETTLEMENT` entry cannot be reversed
  (`ErrNotReversible`), so money that left through a settlement cannot come
  back through the authorization.

Decisions are never revisited when balances are restated. An authorization
approved on a balance that a backdated debit later makes negative stays
approved, and one declined on a balance later restored stays declined.

## 4. What was cut and why

### The store interface is not a migration path

The `Store` interface was kept small so that the append-only rule is carried
by what it lacks. That is its whole value. It does not make a move to a
database easy, and it would be wrong to claim it does. A database-backed
implementation that satisfied it exactly would still be unsafe, because the
interface is missing the things a database is needed for:

| Missing | Consequence |
|---|---|
| **Atomic check-and-hold.** `Authorize` reads, decides, then appends, as separate calls. The only thing making that atomic is the mutex inside one `Ledger`. | Two instances over one store both approve against the same funds. This is the one intentionally failing test, `TestKnownLimitation_NoAtomicCheckAndHold`. The same gap applies to `Settle`, `Reverse` and `CloseDay`. Fixing it changes the interface: a conditional append keyed on the account's last sequence number, or a transaction spanning the read and the append, plus a retry path in the callers. |
| **Snapshots.** `Entries` and `AuthEvents` are two reads with no shared point in time. | A settlement appended between them makes an authorization look both held and settled. Harmless under one mutex, wrong under two writers. |
| **Atomic multi-record writes.** Instalments are n appends; a reversal of an instalment event is n appends; a close is dozens. | A crash part-way leaves a partial set. `CloseDay` can be re-run safely because it decides from what is already stored. Instalments and reversals cannot. |
| **Locking of any kind.** | Nothing serialises two closes of the same day from two instances. |
| **Context and read errors.** Reads return slices, not `(slice, error)`, and nothing takes a `context.Context`. | Every signature changes the day reads can fail or time out. |
| **Ranged reads.** Every read returns an account's full history. | No way to push a value-day or sequence filter down to the database. |
| **A sequence that scales.** One counter under one lock orders every record. | In a database this is a single hot row or a gapless sequence, which serialises all writers. |

### Everything else

| Cut | Risk it defers |
|---|---|
| No persistence | A restart loses the ledger. |
| The closed-day marker and the account registry live in the `Ledger`, not the store | A second instance, or a restart, does not know which days are closed; the `ErrDayClosed` guard is then worth nothing. |
| No idempotency: event ids are not checked for duplicates | Replaying E1 twice credits twice. |
| Single-sided entries, no counter-account | No double-entry proof that money is conserved. Fee income, interest expense and suspense balances do not exist, so the bank's side of every entry is missing. |
| Days are integers; end of day is a manual call | No calendar, cut-off, time zone, weekend or holiday handling; nothing guarantees a close happens. |
| No limit on back-value dating, no approval, no reason, no actor | Section 2. |
| No hold expiry, void, partial capture, incremental authorization or refund | Section 3. Available balance drifts down as dead holds accumulate. |
| Unknown-authorization settlements are refused rather than parked | Funds a scheme obliges the issuer to pay are silently not paid. |
| One flat fee; no tax line; no cap, grace amount or daily limit; no BHD fee | Mischarging. A negative BHD account is charged nothing, with only a notice. |
| A waiver does not record why the trigger went away | Cannot tell a bank error from a customer correction, so cannot apply different policies. |
| Retroactive fees are charged with no review | Conduct risk, section 2. |
| Credit interest only, one rate, no tiers, no day-count convention | Any real product needs more. |
| A single capitalization day, not a schedule | Adjustments written after it are never credited. |
| Two currencies, hard-coded; no exchange rates | Adding a currency is a code change. |
| Reversal only of plain credits and debits, once | No way to correct a settlement, a fee (other than by waiver) or a reversal. |
| Rejection reasons are free text | Cannot be counted or routed reliably. |
| No authentication, authorization of callers, or audit of who did what | Nothing is attributable. |
| Full scans everywhere | Section 1. |
| No metrics, logs or alerts | A stuck close or a growing exception list is invisible. |

## Sources

Retrieved 13 November 2026. Items marked "search result" were read as
excerpts returned by a web search, not as full documents.

1. Central Bank of the UAE, Consumer Protection Standards (PDF; downloaded but
   not readable as text here):
   https://centralbank.ae/media/5crd24gm/cp-standards-pdf.pdf
2. Central Bank of the UAE, Consumer Protection Regulation (PDF, not opened):
   https://www.centralbank.ae/media/vi1klb11/consumer-protection-regulation_0.pdf
3. CBUAE Rulebook mirror, pages of the Consumer Protection Standards (search
   results; the excerpts were not attributed to a single page):
   https://cbuae.thomsonreuters.com/node/1338 and
   https://cbuae.thomsonreuters.com/node/1374
4. CBUAE Rulebook mirror, AML/CFT guidance, "Record Keeping" (search result):
   https://cbuae.thomsonreuters.com/node/981
5. Al Tamimi & Company, on the UAE AML framework after Federal Decree-Law No.
   10 of 2025 (search result):
   https://www.tamimi.com/law-update/general/articles/the-uaes-aml-framework-navigating-a-new-era-of-financial-crime-compliance-in-2026
6. White & Case, "UAE enacts the New CBUAE Law which repeals and replaces the
   2018 Law" (search result):
   https://www.whitecase.com/insight-alert/uae-enacts-new-cbuae-law-which-repeals-and-replaces-2018-law
7. Ashurst, "UAE enacts landmark Central Bank law" (search result):
   https://www.ashurst.com/en/insights/uae-enacts-landmark-central-bank-law
8. CBUAE Rulebook, Retail Payment Services and Card Schemes Regulation
   (search result): https://rulebook.centralbank.ae/node/1580
9. Al Tamimi & Company, "Retail Payment Services and Card Schemes Regulation"
   (search result):
   https://www.tamimi.com/law-update/september-2021/articles/retail-payment-services-and-card-schemes-regulation/
10. Standard Chartered UAE, VAT frequently asked questions (search result):
    https://www.sc.com/global/av/ae-vat-frequently-asked-questions.pdf
11. Gulf News, "How is VAT applied to banking services?" (search result):
    https://gulfnews.com/your-money/taxation/how-is-vat-applied-to-banking-services-1.2147223
