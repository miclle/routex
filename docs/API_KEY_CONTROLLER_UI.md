# API Key Controller Administrator Interface

The Controller interface extends the existing Application targets and Profiles
review workflow. Review a Profile, then choose **Review delivery controllers** to
open compact retained-Slot and enrolled-Controller tables. It introduces no global
Controller directory or alternative navigation layout. All controls use existing
local Tables, Inputs, Switches and Base UI Dialog wrappers.

## Authority and recorded facts

The stable workspace remains mounted outside the Profile area's
Session-generation composition. Every private read requires a fresh intrinsic
administrator Session and independent `secrets.read`; enrollment, assignment and
revocation additionally require independent `secrets.write`. Failed, invalidated
or renewed reads hide prior facts and actions. Actor, Profile, selected Slot ID
and birth, selected Controller and Session/permission generations fence requests,
query publication, reviewed writes and asynchronous clipboard notices.

Retained Slot and Controller lists use the bounded resource-scoped discovery APIs,
with 20 rows per page and opaque cursors. Stored descriptor revisions remain
exact decimal strings. Controller lifecycle labels describe the credential's
recorded lifetime only. A summary explicitly says its configuration has not been
reviewed; current configuration requires the independent exact-Controller read.

Recorded configuration status and the latest exact-revision Controller self-report
are displayed separately. Reported Agent/output states are advisory. They do not
prove Agent output, application use, native inference completion, retirement or
fleet convergence. An expired, revoked or currently unavailable grant remains
locatable; any new revocation requires an explicit reviewed exact-Controller
confirmation and reason.

## Enrollment and configuration changes

Enrollment first reads the exact Slot's current configuration and review token.
The administrator supplies a name, reason, 2–720 hour lifetime and explicit
configuration authorization, then reviews and confirms one captured UUIDv4/body/
strong If-Match. Typing, reading and page refreshes never enroll a Controller.
The UTC expiry is captured once and an identical retry never extends it.

A confirmed new enrollment may return a one-time machine bearer. It stays only
in transient component state and its private lifetime cell, never in Query or
mutation caches, browser storage, logs or an error object. Dismissal, Session
renewal, changed actor/resource and unmount clear it. A stale copy callback must
still match the live displayed bearer before clipboard dispatch, and obsolete
clipboard completions cannot report success. A successful retained enrollment
retry returns no recoverable bearer.

Configuration assignment is a separately reviewed explicit write with its own
UUIDv4, current complete configuration revision/digest, exact Controller review
and reason. Revocation likewise uses the exact Controller's reviewed strong
If-Match and reason. Their confirmed server responses do not imply application
adoption or inference success.

## Uncertain requests

The dedicated Controller lifetime has no automatic five-minute intent expiry.
Same-actor Session renewal keeps an already-dispatched original body, UUID and
If-Match as uncertain, clears bearers and old authority, releases the local
pending state and permits only an identical explicit retry after fresh authority.
An unsuccessful retry cannot erase an earlier uncertain result. A resource or
actor change, explicit dismissal or abandonment clears the original local
material. Synchronous pending/intent locks protect callbacks captured before
React updates disabled controls.

The original-actor enrollment receipt lookup returns nonsecret request/review
metadata and an unavailable bearer. Its metadata cannot reconcile the captured
body; an identical explicit enrollment POST retry confirms that original intent.
Not-found receipt metadata does not prove the original write failed. A generic
Controller read is current configuration review, never historical operation proof.

## Localization and qualification

All labels, confirmations, notices, validation guidance, statuses and accessible
names use the paired `keyControllers` English/Chinese namespace. English remains
the default. Dates follow the selected language, and changing languages preserves
the name and reason draft.

Commit `14a75fe` delivers this interface with the enrolled controller routes.
The hardened focused API, lifetime, UI, parent-workspace and localization suite
passes 56 cases. It covers explicit confirmation, exact request retry, independent
permissions, same-actor renewal, changed Slot birth/Controller, delayed private
responses, stale review proof, uncached one-time bearers and stale clipboard
callbacks. The first focused predecessor retained one incorrect localized-label
test assertion, subsequently corrected; earlier failed runs remain failed evidence.
The hardened source also passes TypeScript with a bounded 2 GiB heap and focused
Prettier/ESLint checks. The containing phase passes mandatory checking, the full
frontend suite, complete PostgreSQL/MySQL regression and authentication restart.
Real-window browser acceptance and external Controller/Agent integration remain
independent pending gates.
