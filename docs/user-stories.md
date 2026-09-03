# Kleinanzeigen web user stories

Research snapshot: 2 September 2026

This is the complete functional story inventory for a consumer or commercial
user moving through the Kleinanzeigen web marketplace. It covers public
discovery, accounts, buying, selling, messaging, transactions, safety, support,
and commercial use. Corporate pages such as careers, press, and advertising
sales are intentionally excluded because they are not marketplace workflows.

This is a reference inventory of what a web user may encounter, not the current
kcli product backlog. It is intentionally broader than kcli and retains selling,
payments, shipping, support, and commercial workflows for traceability. The
current release subset is defined in the [v0.1 scope](v0.1-scope.md), while the
enduring kcli boundary is in [the product scope](kcli-scope.md).

Every item uses one format:

> **US-ID** — As a **role**, I want **a capability or outcome**, so that **I
> receive a clear benefit**.

These are product stories, not claims that the selected mobile API already
supports them. See [API coverage](#mobile-api-coverage) after the inventory.

## Entry, consent, and accessibility

- **US-ENTRY-01** — As a visitor, I want to use the marketplace without registering, so that I can evaluate listings before creating an account.
- **US-ENTRY-02** — As a visitor, I want to choose or reject optional advertising and tracking, so that I control how my data is used.
- **US-ENTRY-03** — As a user, I want to reopen and change my privacy choices, so that my current consent remains under my control.
- **US-ENTRY-04** — As a user, I want to subscribe to or manage Kleinanzeigen Pur, so that I can use the site without third-party advertising.
- **US-ENTRY-05** — As a keyboard or screen-reader user, I want semantic controls, meaningful labels, and a skip-to-content link, so that I can operate the marketplace accessibly.

## Browse, search, and discovery

- **US-DISC-01** — As a visitor, I want to browse the full category hierarchy, so that I can discover items without knowing a search term.
- **US-DISC-02** — As a visitor, I want to search by keyword, so that I can find listings matching what I need.
- **US-DISC-03** — As a visitor, I want to search within a category, so that unrelated listing types are excluded.
- **US-DISC-04** — As a visitor, I want to set a postcode or place, so that results are relevant to my location.
- **US-DISC-05** — As a visitor, I want to choose a search radius or search all Germany, so that I can balance proximity and selection.
- **US-DISC-06** — As a visitor, I want to filter by minimum and maximum price, so that results fit my budget.
- **US-DISC-07** — As a visitor, I want category-specific filters such as condition, type, size, brand, or technical attributes, so that I can narrow results precisely.
- **US-DISC-08** — As a visitor, I want to filter for shipping, pickup, pictures, direct purchase, private sellers, or commercial sellers, so that results match how I want to transact.
- **US-DISC-09** — As a visitor, I want to distinguish offered, wanted, free, and exchange listings, so that I find the correct kind of opportunity.
- **US-DISC-10** — As a visitor, I want to sort by relevance, recency, price, or distance, so that the most useful results appear first.
- **US-DISC-11** — As a visitor, I want to page through result sets and see the approximate total, so that I can explore beyond the first page.
- **US-DISC-12** — As a visitor, I want promoted and commercial listings to be identifiable, so that I understand why they are positioned prominently.
- **US-DISC-13** — As a property seeker, I want to view eligible real-estate listings on a map, so that I can compare location and proximity spatially.
- **US-DISC-14** — As a visitor, I want to browse popular searches, cities, company pages, and newly posted listings, so that I can discover relevant inventory from different entry points.
- **US-DISC-15** — As a member, I want a personalized discovery feed, so that listings aligned with my interests are easier to find.

## Listing details and seller trust

- **US-AD-01** — As a visitor, I want to open a listing by URL or ID, so that I can inspect one offer directly.
- **US-AD-02** — As a visitor, I want to see the title, description, price type, location, date, and listing ID, so that I understand the offer and can refer to it later.
- **US-AD-03** — As a visitor, I want to inspect category-specific attributes, so that I can assess whether the item meets my requirements.
- **US-AD-04** — As a visitor, I want to browse and enlarge the full image gallery, so that I can evaluate condition and details visually.
- **US-AD-05** — As a visitor, I want to see pickup, shipping, direct-buy, and payment availability, so that I know how the item can be acquired.
- **US-AD-06** — As a visitor, I want to inspect the seller's public profile, account type, active-since date, badges, and aggregate rating, so that I can judge trustworthiness.
- **US-AD-07** — As a visitor, I want to browse the seller's other active listings or company page, so that I can evaluate the seller and related inventory.
- **US-AD-08** — As a visitor, I want to see whether a listing is active, reserved, expired, or deleted, so that I do not act on stale inventory.
- **US-AD-09** — As a visitor, I want to share a listing through a stable link, so that another person can review it.
- **US-AD-10** — As a visitor, I want to report a prohibited, misleading, duplicate, or suspicious listing, so that the marketplace can review it.

## Registration, authentication, and account settings

- **US-ACCT-01** — As a new user, I want to register with an email address, telephone number, and password, so that I can use member-only features.
- **US-ACCT-02** — As a new user, I want to verify my email address and telephone number, so that my account can be activated securely.
- **US-ACCT-03** — As a member, I want to log in and log out, so that I can access and protect my account.
- **US-ACCT-04** — As a member, I want to enable and complete two-factor authentication, so that account takeover is harder.
- **US-ACCT-05** — As a member who forgot my password, I want to reset it securely, so that I can regain access.
- **US-ACCT-06** — As a member, I want to change my password, so that I can respond to a security concern.
- **US-ACCT-07** — As a member, I want to change and reverify my email address, so that login and notifications use my current address.
- **US-ACCT-08** — As a member, I want to update my name, telephone number, and address, so that account and transaction information stays correct.
- **US-ACCT-09** — As a member, I want to understand and select the appropriate private or commercial account type, so that I comply with marketplace rules.
- **US-ACCT-10** — As a member, I want to view my public profile, ratings, and earned badges, so that I understand what other users see.
- **US-ACCT-11** — As a member, I want to manage email, push, marketing, message, watchlist, followed-user, and saved-search notifications, so that I receive only useful alerts.
- **US-ACCT-12** — As a member, I want to view my recent purchase and sale transactions, so that I can track marketplace activity.
- **US-ACCT-13** — As a member, I want to manage payment and payout details securely, so that eligible transactions can be completed.
- **US-ACCT-14** — As a member, I want to obtain or erase my personal data and delete my account, so that I can exercise my privacy rights.
- **US-ACCT-15** — As a member with a restricted or compromised account, I want a recovery and review path, so that legitimate access can be restored safely.

## Favorites, saved searches, and following

- **US-SAVE-01** — As a member, I want to add a listing to my watchlist from search or detail, so that I can revisit it.
- **US-SAVE-02** — As a member, I want to view and remove watchlist entries, so that my saved inventory stays useful.
- **US-SAVE-03** — As a member, I want to see price or status changes for watched listings, so that I can react to meaningful updates.
- **US-SAVE-04** — As a member, I want to save the current query and filters as a search alert, so that I do not have to recreate it.
- **US-SAVE-05** — As a member, I want to view, rename, enable, disable, and delete saved searches, so that I control what is monitored.
- **US-SAVE-06** — As a member, I want notifications when new listings match an enabled saved search, so that I can respond quickly.
- **US-SAVE-07** — As a member, I want to follow or unfollow a seller from their profile, so that I can track their new and changed listings.
- **US-SAVE-08** — As a member, I want to view the sellers I follow, so that I can manage this source of recommendations and notifications.

## Messaging and negotiation

- **US-MSG-01** — As a buyer, I want to start a conversation from an active listing, so that I can contact its seller.
- **US-MSG-02** — As a participant, I want to see all conversation threads with unread state and listing context, so that I can prioritize replies.
- **US-MSG-03** — As a participant, I want to open the full message history, so that I understand prior agreements.
- **US-MSG-04** — As a participant, I want to send and receive text messages, so that I can ask questions and coordinate a transaction.
- **US-MSG-05** — As a participant, I want to exchange permitted photos or attachments, so that condition and transaction details can be clarified.
- **US-MSG-06** — As a participant, I want safety warnings for messages containing contact or bank details, so that risky off-platform behavior is visible.
- **US-MSG-07** — As a participant, I want to continue an existing conversation after its listing is reserved or deleted, so that an in-progress agreement is not lost.
- **US-MSG-08** — As a participant, I want to delete unwanted conversations from my inbox, so that I can keep it manageable.
- **US-MSG-09** — As a buyer or seller, I want to propose, accept, reject, or counter a price, so that we can negotiate within the listing context.
- **US-MSG-10** — As a participant, I want to arrange an inspection, pickup, and agreed payment method, so that a local exchange can be completed.

## Creating and managing listings

- **US-SELL-01** — As a seller, I want to start a new offered, wanted, free, exchange, service, property, vehicle, job, or other category listing, so that I can reach the appropriate audience.
- **US-SELL-02** — As a seller, I want the category to be suggested from my title and changeable by me, so that the listing uses the right fields and placement.
- **US-SELL-03** — As a seller, I want to add a clear title and description, so that buyers understand the offer.
- **US-SELL-04** — As a seller, I want optional AI assistance drafting the description, so that I can create a useful listing more quickly.
- **US-SELL-05** — As a seller, I want to upload, reorder, preview, and remove photos, so that the gallery presents the item accurately.
- **US-SELL-06** — As a seller, I want to set a fixed, negotiable, free, or category-appropriate price, so that transaction expectations are clear.
- **US-SELL-07** — As a seller, I want to complete required and optional category attributes, so that buyers can filter and evaluate the listing.
- **US-SELL-08** — As a seller, I want to set the listing location and permitted contact details, so that buyers know where and how the exchange can occur.
- **US-SELL-09** — As a seller, I want to choose pickup or supported shipping options, package size, and delivery method, so that fulfillment terms are accurate.
- **US-SELL-10** — As a seller, I want to enable or disable direct purchase where eligible, so that I control whether negotiation is required.
- **US-SELL-11** — As a seller, I want to preview and validate the complete listing before publication, so that errors and missing required data can be corrected.
- **US-SELL-12** — As a seller, I want to save, continue, or delete a draft, so that I can prepare a listing over multiple sessions.
- **US-SELL-13** — As a seller, I want to publish a validated listing and see its moderation status, so that I know when it becomes discoverable.
- **US-SELL-14** — As a seller, I want to view all my drafts, active, reserved, expired, and deleted listings, so that I can manage their lifecycle.
- **US-SELL-15** — As a seller, I want to edit eligible listing content, attributes, images, price, and fulfillment options, so that the public offer remains accurate.
- **US-SELL-16** — As a seller, I want to reserve and later reactivate a listing, so that I can pause inquiries without deleting my work.
- **US-SELL-17** — As a seller, I want to mark an item sold or delete its listing, so that unavailable inventory leaves search.
- **US-SELL-18** — As a seller, I want to extend an eligible listing before expiry, so that it remains online without being recreated.
- **US-SELL-19** — As a seller, I want to recreate an expired or deleted listing from retained content, so that I can relist something that cannot be restored directly.
- **US-SELL-20** — As a seller, I want to purchase eligible promotion such as highlighting, top placement, or bumping, so that more buyers see my listing.
- **US-SELL-21** — As a seller, I want to review promotion status, payment failures, and invoices, so that I can manage paid listing services.

## Buyer purchase and fulfillment

- **US-BUY-01** — As a buyer, I want to inspect the seller, listing, price, delivery terms, and protection eligibility before committing, so that I can assess the transaction risk.
- **US-BUY-02** — As a buyer, I want to buy an eligible item directly at the stated price, so that I can secure it without negotiation.
- **US-BUY-03** — As a buyer, I want to submit a price offer and receive the seller's decision or counteroffer, so that we can agree on terms.
- **US-BUY-04** — As a buyer, I want to choose an available delivery or pickup option, so that fulfillment suits my needs.
- **US-BUY-05** — As a buyer, I want to enter or select a delivery address before payment, so that the package goes to the correct place.
- **US-BUY-06** — As a buyer, I want to see item price, shipping charge, buyer-protection fee, and total before payment, so that there are no hidden costs.
- **US-BUY-07** — As a buyer, I want to pay using an available supported method, so that I can complete a protected purchase.
- **US-BUY-08** — As a buyer, I want clear recovery guidance when payment is rejected or interrupted, so that I can safely retry or choose another method.
- **US-BUY-09** — As a buyer, I want to see transaction status and shipment tracking in my account and conversation, so that I know what happens next.
- **US-BUY-10** — As a buyer, I want to confirm delivery and, where requested, that the item is satisfactory, so that the seller can be paid.
- **US-BUY-11** — As a buyer, I want to cancel when the transaction is still cancellable, so that funds can be returned when fulfillment will not happen.
- **US-BUY-12** — As a buyer, I want to report non-delivery, damage, counterfeit goods, missing contents, or material misdescription before the protection deadline, so that payout pauses while the issue is reviewed.
- **US-BUY-13** — As a buyer, I want to negotiate a return, partial refund, or full refund and supply evidence, so that a transaction problem can be resolved fairly.
- **US-BUY-14** — As a buyer, I want to track an approved refund, so that I know when my money will arrive.

## Seller order, shipping, and payout

- **US-ORDER-01** — As a seller, I want to receive a trustworthy notification that an offer or direct purchase was paid, so that I do not ship based on forged messages.
- **US-ORDER-02** — As a seller, I want to accept, reject, or counter an offer, so that I retain control over the sale price.
- **US-ORDER-03** — As a seller, I want to confirm item availability within the required period, so that a paid order can proceed.
- **US-ORDER-04** — As a seller, I want to cancel an eligible transaction, so that the buyer is refunded when I cannot fulfill it.
- **US-ORDER-05** — As a seller, I want to obtain the integrated DHL or Hermes shipping label, so that I can dispatch with platform tracking.
- **US-ORDER-06** — As a seller, I want to see packaging, size, weight, insurance, and handoff instructions, so that the shipment meets carrier requirements.
- **US-ORDER-07** — As a seller, I want to provide tracking for another agreed shipping method, so that the buyer can follow delivery.
- **US-ORDER-08** — As a seller, I want to see shipment and delivery status, so that I can detect fulfillment problems.
- **US-ORDER-09** — As a seller, I want to add, verify, change, or remove my payout IBAN, so that proceeds reach the correct account.
- **US-ORDER-10** — As a seller, I want to complete required identity verification securely, so that held payouts can be released.
- **US-ORDER-11** — As a seller, I want to see pending, held, released, and completed payout states, so that I can reconcile sales.
- **US-ORDER-12** — As a seller, I want to respond to a buyer's problem report and provide evidence, so that the case can be resolved fairly.
- **US-ORDER-13** — As a seller, I want to accept, reject, or counter a proposed return, price reduction, or refund, so that we can reach an agreed resolution.
- **US-ORDER-14** — As a seller, I want to provide a return address and confirm returned-item receipt, so that an agreed return and refund can complete.
- **US-ORDER-15** — As a seller, I want to provide legally required tax-transparency information when applicable, so that eligible sales comply with reporting obligations.

## Reputation, safety, and support

- **US-SAFE-01** — As a participant, I want to rate an eligible counterparty after sufficient interaction or a completed transaction, so that future users have a trust signal.
- **US-SAFE-02** — As a member, I want to understand my ratings and request review of an abusive or invalid rating, so that my reputation is treated fairly.
- **US-SAFE-03** — As a participant, I want to report a user or individual message with a reason, so that abusive or fraudulent behavior can be investigated.
- **US-SAFE-04** — As a participant, I want to block a user for the relevant conversation or listing, so that further unwanted messages stop.
- **US-SAFE-05** — As a participant, I want prominent phishing and off-platform-payment warnings, so that I can recognize common fraud attempts.
- **US-SAFE-06** — As a member, I want to report suspected account takeover and revoke unsafe access, so that I can contain a compromise.
- **US-SAFE-07** — As a buyer who paid outside the protected flow, I want fraud-reporting and evidence guidance, so that I can pursue the appropriate platform, payment-provider, or legal remedy.
- **US-SAFE-08** — As a seller, I want a clear explanation and appeal path when my listing is rejected or removed, so that mistakes can be corrected.
- **US-SAFE-09** — As a member, I want a clear explanation and recovery path when my account is restricted, so that legitimate use can resume.
- **US-SAFE-10** — As a user, I want to search help content and submit a support request with relevant listing, conversation, or transaction context, so that my issue reaches the correct team.
- **US-SAFE-11** — As a customer of a paid feature or subscription, I want to exercise an applicable cancellation or withdrawal right, so that the contract can end correctly.
- **US-SAFE-12** — As a user, I want to read marketplace rules, privacy terms, youth-protection guidance, and accessibility information, so that I understand my rights and responsibilities.

## Commercial and PRO users

- **US-PRO-01** — As a commercial seller, I want to identify myself correctly and publish legally required business information, so that my presence complies with commercial obligations.
- **US-PRO-02** — As a commercial seller, I want a public company page or shop with my inventory, so that buyers can browse my business offerings together.
- **US-PRO-03** — As a commercial seller, I want packages and scalable listing-management options, so that I can maintain larger vehicle, property, job, or goods inventories.
- **US-PRO-04** — As a commercial seller, I want billing records and invoices for paid services, so that business expenditure can be reconciled.
- **US-PRO-05** — As a commercial seller, I want commercial-user support and guidance, so that account or product issues reach specialists familiar with my plan.

## Mobile API coverage

The selected undocumented mobile API is a partial implementation path for these
stories, not a product-completeness boundary:

| Story area | Current API coverage |
|---|---|
| Public location, category, search, filters, results, listing details, and images | Strong for core data; no map or presentation layer |
| Registration, consent, 2FA, settings, privacy, Pur, and account recovery | Not covered beyond Auth0 login and token refresh |
| Watchlist | Read only; add/remove and notifications are missing |
| Saved searches and followed sellers | Not covered |
| Conversations | List, read, reply, mark read, and create are covered; attachments, deletion, warnings, reporting, and blocking are missing |
| Own listings | List, detail, create, reserve/reactivate, extend, and delete are covered |
| Drafts, local image upload, editing, sold state, promotion, and invoices | Not covered |
| Offers, direct purchase, payment, shipping, payout, returns, and disputes | Not covered and should remain in the official UI |
| Ratings, safety reports, appeals, and support requests | Not covered |
| PRO account and bulk-management features | Not covered |

The exact known endpoint contracts are in the
[mobile API reference](mobile-api.md). Use these stories as a traceability
catalog and record each considered story as `supported`, `partial`,
`official-UI-only`, or `out-of-scope`; inclusion here does not place a story in
kcli. Never silently reinterpret an uncovered story as an inferred endpoint.

## Sources and validation

The anonymous home, search-result, and listing-detail pages were inspected in a
fresh browser session. They exposed category browsing, public search and filters,
saved-search prompts, listing galleries and attributes, seller profiles and
badges, watchlist and follow controls, sharing, reporting, and login-gated
contact actions.

The authenticated and transaction inventory was cross-checked against the
current official help-center categories:

- [Listings](https://hilfe.kleinanzeigen.de/hc/de/categories/17006957523740-Anzeigen)
- [User account](https://hilfe.kleinanzeigen.de/hc/de/categories/17007005430684-Nutzerkonto)
- [Safety](https://hilfe.kleinanzeigen.de/hc/de/categories/17007207150620-Sicherheit)
- [Buying with "Sicher bezahlen"](https://hilfe.kleinanzeigen.de/hc/de/categories/17007034037148-Kaufen-mit-Sicher-bezahlen)
- [Selling with "Sicher bezahlen"](https://hilfe.kleinanzeigen.de/hc/de/categories/17007052627356-Verkaufen-mit-Sicher-bezahlen)

No login, message, listing mutation, payment, or transaction action was performed
while producing this inventory.
