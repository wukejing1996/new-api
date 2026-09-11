# Hot model management

The default admin UI exposes **System Settings → Models & Routing → Hot Model Management** at `/system-settings/models/hot-models`. It uses the existing system-settings access guard. The API uses `AdminAuth`.

- `GET /api/hot-models/` lists distinct enabled `abilities.model` values, including models without a `models` metadata record.
- `PUT /api/hot-models/` accepts `{ "model_name": "vendor/model", "is_hot": true }`. Explicit `false` removes the hot flag. Unavailable models and invalid payloads are rejected.
- Preferences are stored in `hot_models`, keyed by the exact model name. They are independent of model metadata, price options and individual channels.
- `/api/pricing` includes `is_hot` and `created_time`. CostRouter controls the public display and defaults to hot first, then newest entry, then name.

## Entry timestamps and upgrade

The migration backfills existing channel models with `created_time = 0` (unknown). Neither a channel's creation time nor a metadata record's creation time proves when a model was first added to a channel.

After upgrade, channel ability creation/update also registers newly seen model names in the catalog. Existing timestamps and hot flags are never overwritten by channel updates or ability rebuilds. Removing a channel does not delete catalog preferences.

The migration copies legacy `models.is_hot` values for exact-name metadata records where a new preference does not yet exist. It leaves that old database column intact for rollback safety. Repeated migrations do not overwrite settings saved in the new page. Prefix/suffix metadata flags are not inherited by unrelated model names.

Deploy the backend (including migrations) and the rebuilt default administrator frontend, then the CostRouter frontend. No production data is changed by merely editing these source files. Existing pricing caches can delay public display updates; saving invalidates the backend pricing cache, while CostRouter's server cache retains its existing refresh interval.

## Checks

```sh
go test ./model ./controller -run TestHotModel -count=1
```

Tests cover an empty metadata table, duplicate channels, explicit unmarking, entry-date preservation, unavailable models, the public pricing fields, and repeatable legacy migration. Channel routing and billing configuration retain their existing behavior; the additional catalog registration participates in the supplied channel transaction.
