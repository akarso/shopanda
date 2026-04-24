# Phase 3 — Runtime Refactor Roadmap

## Strategy

* Track larger runtime and behavioral refactors separately from narrow bug-fix PRs
* Keep each implementation PR scoped to one production behavior change, even when it advances a larger refactor track
* Prefer vertical slices that fix the user-visible inconsistency first, then widen into domain changes only when required

---

## Refactor Tracks

| Track | Status | Goal | Notes |
| --- | --- | --- | --- |
| 1 | done in PR-306 | Guest cart continuity across login/register | Anonymous cart is claimed or merged into the authenticated customer's active cart, and the guest cart cookie is cleared so storefront surfaces stay in sync |
| 2 | planned | Guest checkout without account creation | Let anonymous customers complete checkout without a customer account while preserving a later path to account creation or order claiming |

---

## Track 2 — Guest Checkout Without Account

### Goal

Allow an anonymous shopper to move from cart to completed order without being forced to create or log into an account.

### Why This Is Separate

Track 1 only fixes cart ownership continuity. Track 2 changes checkout and order semantics:

* checkout currently requires a non-empty `customerID`
* order creation currently requires a non-empty `customerID`
* storefront checkout currently marks authentication as required before confirmation

That is a broader domain refactor than the guest-cart handoff and should stay isolated from it.

### Expected Scope

* checkout service accepts guest checkout inputs without a customer identity
* order domain and persistence stop assuming every order has a registered customer id
* storefront checkout pages remove the hard account gate and switch to guest-capable messaging and validation
* notification and account flows define how a guest order can later be attached to an account or discovered safely
* operational logging and tests cover both guest and authenticated checkout paths

### Design Constraints

* guest checkout must not break existing authenticated checkout
* account-based order history must remain correct for registered users
* guest orders need a recoverable identity model, likely based on email plus explicit claim or link flows rather than implicit account creation
* cart ownership and order ownership must remain explicit; no hidden reassignment across customers

### Open Questions

* should guest checkout create a lightweight customer record, or should orders support `customer_id` being empty?
* what is the post-purchase account-linking flow: passwordless claim, explicit registration, or manual merge?
* what is the minimum guest identity snapshot required on the order for support and notification flows?

### Validation Target

When Track 2 ships, the same catalog/cart should support both:

* anonymous shopper: cart -> checkout -> order confirmation
* authenticated shopper: cart -> checkout -> order confirmation

without divergent pricing, stock reservation, or notification behavior.