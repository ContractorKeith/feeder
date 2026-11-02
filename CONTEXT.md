# feeder

Language for feeder's personal reading briefs.

## Language

**Digest**:
A personal reading brief containing selected feed items, each with a feed excerpt or an opt-in model summary and its source link.
_Avoid_: Newsletter, combined narrative

**Feed item**:
An entry supplied by an RSS or Atom feed, including the content that the feed provides. It does not imply access to the full linked article.
_Avoid_: Full article, scraped page

**First-seen time**:
The time feeder first stores a feed item, used to determine its eligibility for a digest window rather than the publisher's date.
_Avoid_: Publication date, article date

**Daily digest**:
A digest for the previous calendar day in the effective timezone.
_Avoid_: Last 24 hours

**Weekly digest**:
A digest for the previous Monday–Sunday calendar week in the effective timezone. Items may also have appeared in daily digests.
_Avoid_: Last seven days, unread digest

**Baseline item**:
A feed item discovered during a subscription's initial fetch, stored but excluded from normal digests unless explicitly included.
_Avoid_: New arrival, unread item

**Subscription removal**:
Stopping future fetches of a feed while retaining its stored items for historical digests.
_Avoid_: Purge, delete history

**Purge**:
Explicit deletion of retained data, separate from removing a subscription.
_Avoid_: Unsubscribe, pause

**Excerpt**:
Text drawn from feed-provided content without model summarization. It is the default digest entry text and the labeled fallback when a requested summary fails.
_Avoid_: Model summary, full article

**Summary**:
An opt-in model-generated condensation of feed-provided content, associated with its source link.
_Avoid_: Excerpt, verified full-article analysis

**Degraded digest**:
A produced digest in which a requested summary failed and was replaced by a labeled excerpt, with a distinct exit status for automation.
_Avoid_: Complete success, empty digest
