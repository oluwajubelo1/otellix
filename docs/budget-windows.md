# Budget Windows

A budget limit only means something once you say *over what period*. "$5 per user" could mean per day, per month, or per the last 30 days. In Otellix, that period is the **budget window**.

This guide covers the three kinds of window, how to pick one, how to set one up with each store, and how resets work.

> New to budgets? Read [Budget Guardrails](budget-guardrails.md) first. It covers limits, fallback actions and stores. This guide builds on it.

---

## Quick start

A $10 monthly budget per user that resets at midnight UTC on the 1st:

```go
import (
    "time"

    "github.com/oluwajubelo1/otellix"
)

monthly := otellix.CalendarMonth(time.UTC)

budget := &otellix.BudgetConfig{
    PerUserLimit:   10.00,
    FallbackAction: otellix.FallbackBlock,
    Window:         &monthly,
}

result, err := otellix.Trace(ctx, provider, params,
    otellix.WithUserID("user_123"),
    otellix.WithBudgetConfig(budget),
)
```

This uses the in-memory store, which loses its data on restart. For anything beyond local development, use Redis: see [Using a window with Redis](#using-a-window-with-redis).

---

## The three kinds of window

| Window | Constructor | Spend that counts | When spend resets |
| --- | --- | --- | --- |
| Rolling | `otellix.RollingWindow(d)` | Everything in the last `d` | Gradually: each call drops out exactly `d` after it was made |
| Calendar day | `otellix.CalendarDay(loc)` | Everything since midnight today, in `loc` | All at once, at the next midnight in `loc` |
| Calendar month | `otellix.CalendarMonth(loc)` | Everything since 00:00 on the 1st of this month, in `loc` | All at once, at 00:00 on the 1st of next month in `loc` |

`loc` is a `*time.Location`. Passing `nil` means UTC.

If you configure nothing, Otellix uses a **rolling 24-hour window**. That's the behaviour Otellix has always had.

### Rolling windows

A rolling window looks back a fixed length of time from *now*. With `RollingWindow(24 * time.Hour)`, a call at 14:00 counts until 14:00 tomorrow, then drops out.

```text
Rolling 24h window, checked at 14:00 Tuesday

   Mon 14:00                                Tue 14:00 (now)
       |<--------------- counts ---------------->|
  ...  x   x        x           x     x      x
       ^ calls before Mon 14:00 no longer count
```

Rolling windows have no "reset moment". A user who hits their limit gets budget back gradually, as their oldest calls age out. That makes them good for rate-style limits ("no more than $5 in any 24 hours"), because there's no midnight rush where everyone's budget refills at once.

Common rolling windows:

```go
otellix.RollingWindow(time.Hour)           // last hour
otellix.RollingWindow(24 * time.Hour)      // last 24 hours (the default)
otellix.RollingWindow(7 * 24 * time.Hour)  // last 7 days
otellix.RollingWindow(30 * 24 * time.Hour) // last 30 days
```

`RollingWindow(0)` or a negative duration means the default, 24 hours.

### Calendar windows

A calendar window follows the calendar. It starts at a fixed boundary and everything resets at the next one.

```text
CalendarMonth(time.UTC), checked on 21 October

   1 Oct 00:00                21 Oct (now)          1 Nov 00:00
       |<--------- counts --------->|                    |
  ...  x    x   x        x    x     x                    ^ everything resets
       ^ September's calls never count, however recent
```

Calendar windows match how people think about billing: "this month's AI budget", "today's allowance". They're the right choice when the limit has to line up with an invoice, a subscription period or a finance report.

### Which one should I use?

| You want... | Use |
| --- | --- |
| A limit that lines up with your monthly provider invoice | `CalendarMonth` |
| A "daily allowance" that refills at a predictable time for users | `CalendarDay` |
| Protection against bursts, e.g. no more than $2 in any hour | `RollingWindow(time.Hour)` |
| A long, smooth limit without a reset-day rush | `RollingWindow(30 * 24 * time.Hour)` |
| The behaviour you had before windows existed | Nothing: the default is `RollingWindow(24 * time.Hour)` |

---

## Time zones

Calendar windows need to know *whose* midnight. By default it's UTC. If your users or your finance team work in another time zone, pass it in:

```go
lagos, err := time.LoadLocation("Africa/Lagos")
if err != nil {
    log.Fatal(err)
}

window := otellix.CalendarMonth(lagos) // resets at 00:00 Lagos time on the 1st
```

This makes a real difference at the edges. At 23:30 UTC on 31 October it's already 00:30 on 1 November in Lagos (UTC+1), so:

- `CalendarMonth(time.UTC)` is still counting October's spend.
- `CalendarMonth(lagos)` has already reset and is counting November's.

`time.LoadLocation` reads the system's time zone database. Minimal container images (`scratch`, some `distroless` images) don't include one. If `LoadLocation` fails there, add `import _ "time/tzdata"` to your `main` package to embed the database in your binary.

Daylight saving time is handled for you: a calendar day in a zone with DST can be 23 or 25 hours long, and the window follows the local calendar, not a fixed 24 hours.

### Users in many time zones

A calendar window has **one** time zone. Every user it limits shares the same midnight, so a daily budget set to Lagos time resets at 08:00 for a user in Tokyo, and at 18:00 the evening before (19:00 in summer) for a user in New York.

The fair way to limit users around the world is a **rolling window**. "$1 in any 24 hours" means the same thing in every time zone, and there's no reset moment to game. Keep calendar windows for project and organisation budgets, which usually follow a billing month. Use the time zone your billing uses, usually UTC.

Give user limits and project limits their own stores, each with its own window:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:    1.00,  // per user, in any 24 hours
    PerProjectLimit: 500.00, // per project, per calendar month
    UserStore:       otellix.NewInMemoryBudgetStore(24 * time.Hour),
    ProjectStore:    otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarMonth(time.UTC)),
}
```

With Redis, both stores can share a client and a prefix. Their keys don't overlap:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:    1.00,
    PerProjectLimit: 500.00,
    UserStore:       otellixredis.NewRedisBudgetStore(rdb, "otellix:budget", 24*time.Hour),
    ProjectStore:    otellixredis.NewRedisBudgetStoreWithWindow(rdb, "otellix:budget", otellix.CalendarMonth(time.UTC)),
}
```

`UserStore` holds per-user spend and the shared `UnattributedLimit`; `ProjectStore` holds per-project spend. Either falls back to `Store` when it isn't set. A call is still checked against both before it runs: Otellix holds the estimate in the user store, then in the project store, and releases the first if the second doesn't fit.

> **Why not each user's own midnight?** Users can usually change their time zone in your app's settings. If budgets reset at each user's local midnight, moving the setting a few zones east would start a "new day" and a fresh budget, as often as the user liked. A rolling window can't be reset that way.

### Every instance needs the same window

All instances of your app that share a Redis prefix must use the same window, including the same time zone. Load it by name everywhere, e.g. `time.LoadLocation("Africa/Lagos")`, never `time.Local`: `time.Local` is whatever zone each server happens to be set to, so two servers can disagree about when a day starts.

If instances do disagree, no spend is lost: the Redis store only deletes spend that no window could still count. But they enforce different periods, so a user can be blocked by one instance and allowed by another. To catch this, each instance records its window in Redis the first time it uses each kind of budget, and reports a mismatch through the OpenTelemetry error handler:

```text
otellix: Redis budget store "otellix:budget": this instance counts user budgets over a calendar day in Africa/Lagos window,
but another instance using the same prefix counts them over a calendar day in UTC window. No spend is lost, but the
instances disagree about when budgets reset; give every instance the same window
```

A Redis store whose calendar window uses `time.Local` also reports a warning when it's created.

> **Fixed in v1.8.0:** the Redis store deleted spend from before *its own* window's start. An instance whose day started later, such as one in a time zone further east, deleted spend that other instances still counted: in a test, $1.00 of spend dropped to $0.00.

---

## Setting the window

Where you set the window depends on which store you use. **The store owns the window.** Every limit that uses that store (per user, per project and unattributed) shares its window. To give user and project limits different windows, use `UserStore` and `ProjectStore`: see [Users in many time zones](#users-in-many-time-zones).

### Using the default in-memory store

If you don't pass a `Store`, Otellix creates an in-memory one for you. Set `BudgetConfig.Window` to choose its window. It's a pointer, so declare the window first:

```go
weekly := otellix.RollingWindow(7 * 24 * time.Hour)

budget := &otellix.BudgetConfig{
    PerUserLimit: 3.00,
    Window:       &weekly,
}
```

`BudgetConfig.ResetInterval` still works and sets a rolling window of that length. If you set both, `Window` wins.

### Creating an in-memory store yourself

```go
store := otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarDay(time.UTC))

budget := &otellix.BudgetConfig{
    Store:        store,
    PerUserLimit: 0.50,
}
```

`otellix.NewInMemoryBudgetStore(d)` still exists and is the same as `NewInMemoryBudgetStoreWithWindow(otellix.RollingWindow(d))`.

When you pass a `Store`, `BudgetConfig.Window` and `ResetInterval` are ignored. Configure the window on the store.

### Using a window with Redis

Use Redis whenever you run more than one instance of your app, or whenever your window is longer than a few hours. In-memory spend is lost every time the process restarts, so a monthly budget kept in memory resets on every deploy.

```go
import (
    "time"

    "github.com/redis/go-redis/v9"

    "github.com/oluwajubelo1/otellix"
    otellixredis "github.com/oluwajubelo1/otellix/stores/redis"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

store := otellixredis.NewRedisBudgetStoreWithWindow(
    rdb,
    "otellix:budget",            // key prefix
    otellix.CalendarMonth(time.UTC),
)

budget := &otellix.BudgetConfig{
    Store:           store,
    PerUserLimit:    10.00,
    PerProjectLimit: 500.00,
    FallbackAction:  otellix.FallbackBlock,
}
```

`otellixredis.NewRedisBudgetStore(rdb, prefix, d)` still exists and uses a rolling window of length `d`.

To try this locally, start Redis with `docker-compose -f docker-compose.dev.yml up -d`.

---

## Resets and `ResetAt`

When a call is blocked, `BudgetExceededError.ResetAt` tells you when the user gets budget back. What it means depends on the window:

| Window | `ResetAt` is... | In time zone |
| --- | --- | --- |
| `CalendarDay` / `CalendarMonth` | The start of the next period, e.g. `2026-11-01T00:00:00Z`. Everything resets then. | The window's |
| `RollingWindow(d)` | When the oldest call still in the window drops out. Only *that* call's cost comes back at that moment, so the user may still be over the limit afterwards. | UTC |

Use it to tell users when they can try again:

```go
result, err := otellix.Trace(ctx, provider, params,
    otellix.WithUserID(userID),
    otellix.WithBudgetConfig(budget),
)

var budgetErr *otellix.BudgetExceededError
if errors.As(err, &budgetErr) {
    return fmt.Errorf("you've used $%.2f of your $%.2f monthly AI budget; it resets on %s",
        budgetErr.CurrentSpend, budgetErr.Limit,
        budgetErr.ResetAt.Format("2 January"))
}
```

If both a user limit and a project limit apply, the error describes whichever one has the least remaining. See [Budget Guardrails](budget-guardrails.md#which-limit-does-the-error-report).

### Showing reset times to users

`ResetAt` is a moment in time. Its time zone (the window's, or UTC) only affects how it prints. Convert it to the user's time zone before you show it, so a user in Tokyo isn't told their budget resets at midnight when it's 09:00 for them:

```go
loc, err := time.LoadLocation(user.TimeZone) // e.g. "Asia/Tokyo", from your user's profile
if err != nil {
    loc = time.UTC
}
msg := fmt.Sprintf("Your AI budget resets at %s.", budgetErr.ResetAt.In(loc).Format("Mon 2 Jan, 15:04 MST"))
```

For an HTTP API, send the wait as a `Retry-After` header instead. It's a number of seconds, so no time zone is involved:

```go
w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(budgetErr.ResetAt).Seconds())+1))
w.WriteHeader(http.StatusTooManyRequests)
```

> **Changed in v1.8.0:** for rolling windows, `ResetAt` used to be in the server's local time zone, so the same budget printed differently on servers in different zones. It's now in UTC.

---

## How the stores keep window data

You don't need this section to use windows, but it explains the behaviour you'll see in production.

### Redis

- Each user, project or unattributed budget is one Redis **sorted set**. Each call is one entry, scored by the time it was made.
- Every read and write runs in an atomic Lua script, so all app instances agree on the total. Reads count the entries from the window's start.
- Entries are only deleted once no window of that kind could still count them: older than 25 hours for `CalendarDay` in any time zone, 31 days and an hour for `CalendarMonth`, or the window's length for a rolling window. That way an instance with a different window can't delete spend another instance counts.
- Each instance records its window under `<prefix>:_window:<kind>` (`user`, `project` or `unattributed`), to [notice instances that disagree](#every-instance-needs-the-same-window).
- Each key expires once nothing in it can still count: the window's maximum length plus one hour. That's 25 hours for a 24-hour rolling window, about 32 days for `CalendarMonth`, and so on. Keys for users who stop calling clean themselves up.

> **Fixed in this release:** earlier versions expired every Redis key after a fixed 48 hours. With a window longer than that, a user who made no calls for two days had their spend history deleted, and their "monthly" spend dropped to zero. Keys now live as long as the window needs.

### In memory

- Calls made close together are merged into one entry to keep memory bounded:
  - **Calendar windows:** all calls in the same period merge into one entry, so each user needs one entry per period.
  - **Rolling windows:** calls within 1/1000 of the window length merge (about 86 seconds for 24 hours, about 43 minutes for 30 days). Each user needs at most about 1,000 entries, however busy they are.
- A merged entry drops out when its *latest* call leaves the window. Spend can therefore count for up to 1/1000 of the window longer than it strictly should. This errs towards blocking, never towards overspending.
- **Nothing survives a restart.** Use Redis for long windows.

---

## Migrating from `PerUserDailyLimit` / `PerProjectDailyLimit`

These fields were named "Daily", but they always applied to the configured window, which didn't have to be a day. They've been replaced by names that say what they do:

| Old field (deprecated) | New field |
| --- | --- |
| `PerUserDailyLimit` | `PerUserLimit` |
| `PerProjectDailyLimit` | `PerProjectLimit` |

The old fields still work, so existing code keeps compiling and behaving the same. If you set both, the new field wins. To migrate, rename the fields:

```go
// Before
budget := &otellix.BudgetConfig{
    PerUserDailyLimit:    1.00,
    PerProjectDailyLimit: 25.00,
}

// After: identical behaviour
budget := &otellix.BudgetConfig{
    PerUserLimit:    1.00,
    PerProjectLimit: 25.00,
}
```

Linters such as `staticcheck` will flag the old names, which makes them easy to find.

---

## Troubleshooting

**My monthly budget resets on every deploy.**
You're using the in-memory store, which lives inside the process. Switch to `NewRedisBudgetStoreWithWindow`.

**I set `BudgetConfig.Window`, but it has no effect.**
You're also passing `Store`. The store owns the window, so set it with `NewInMemoryBudgetStoreWithWindow` or `NewRedisBudgetStoreWithWindow` instead.

**Spend reset at a strange hour.**
Calendar windows default to UTC. Pass your time zone: `otellix.CalendarDay(lagos)`.

**`time.LoadLocation` fails in my container.**
The image has no time zone database. Add `import _ "time/tzdata"` to your `main` package.

**A blocked user is still blocked after `ResetAt`.**
With a rolling window, `ResetAt` is when the *oldest* call drops out, which may not free enough budget. Calendar windows reset fully at `ResetAt`.

**Can I have a weekly calendar window?**
Not yet: the calendar options are day and month. Use `RollingWindow(7 * 24 * time.Hour)` for a weekly limit. Open an issue if you need it.

**Can users and projects have different windows?**
Yes: set `UserStore` and `ProjectStore`, each with its own window. See [Users in many time zones](#users-in-many-time-zones).

**Can each user have their own midnight?**
No, deliberately: users could reset their budget by changing their time zone. Use a rolling window for per-user limits. See [Users in many time zones](#users-in-many-time-zones).

**The logs say another instance counts budgets over a different window.**
Two instances sharing a Redis prefix are configured with different windows or time zones. Give them all the same window, loaded by name. See [Every instance needs the same window](#every-instance-needs-the-same-window). After you change the window on purpose, the first instance to start reports this once; that's expected.

**The logs warn that a Redis store uses `time.Local`.**
Replace `time.Local` with a named location, e.g. `time.UTC` or `time.LoadLocation("Africa/Lagos")`.

---

## API reference

| Name | Description |
| --- | --- |
| `otellix.RollingWindow(d time.Duration) BudgetWindow` | Spend in the last `d`. `d <= 0` means 24 hours. |
| `otellix.CalendarDay(loc *time.Location) BudgetWindow` | Spend since midnight in `loc` (nil = UTC). |
| `otellix.CalendarMonth(loc *time.Location) BudgetWindow` | Spend since the 1st of the month in `loc` (nil = UTC). |
| `BudgetWindow.Start(now) time.Time` | The earliest moment whose spend counts at `now`. |
| `BudgetWindow.ResetAt(now, oldest) time.Time` | When spend next drops. Calendar windows ignore `oldest`. |
| `BudgetWindow.MaxAge() time.Duration` | The longest a call can keep counting. Stores use it to expire data. |
| `BudgetWindow.Location() *time.Location` | The time zone a calendar window resets in, or nil for a rolling window. |
| `BudgetWindow.String() string` | A description, e.g. `"calendar month in Africa/Lagos"`. |
| `otellix.NewInMemoryBudgetStoreWithWindow(w)` | In-memory store with window `w`. |
| `otellixredis.NewRedisBudgetStoreWithWindow(client, prefix, w)` | Redis store with window `w`. |
| `BudgetConfig.Window *BudgetWindow` | Window for the default in-memory store. Ignored when `Store` is set. |
| `BudgetConfig.UserStore`, `BudgetConfig.ProjectStore` | Separate stores, so user and project limits can have different windows. Default: `Store`. |
| `otellix.DefaultBudgetInterval` | The default rolling window length: 24 hours. |

The zero value `otellix.BudgetWindow{}` is a rolling 24-hour window.
