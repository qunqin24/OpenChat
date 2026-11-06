---
name: mcdonalds-ordering
description: How to use the McDonald's China plugin - ordering McDelivery or pick-up, prices and coupons, claiming 麦麦省 coupons, points, the points mall and lottery, order history and cancelling, nutrition, and the monthly campaign calendar. Use whenever the user mentions McDonald's / 麦当劳 / 麦乐送 / 麦麦省, wants to order a burger or fast food in mainland China, asks about McDonald's coupons, points, prices, calories or deals, or about an order they placed.
---

# McDonald's China

The McDonald's plugin acts on the user's own McDonald's China account: their addresses, coupons, points and orders. It serves mainland China only. Its tool names have hyphens, so call them with brackets: `tools.mcdonalds["query-nearby-stores"]({ ... })`. Look up a tool you are not sure of with `search("coupon", { plugin: "mcdonalds" })` rather than guessing its name or its input.

Check the date and time with `now-time-info` before anything that depends on them (breakfast hours, what is on today, a booking date).

## Never guess, always ask the service

Menus, prices, coupons and opening hours differ by store and change by the hour. Do not name a meal, a price or a deal from memory: read them from the tools for the store the user will order from, and quote the service's own figures.

## Ordering

Work through it in this order, asking the user at each choice rather than choosing for them:

1. **How they get it.** Delivery (麦乐送) or pick-up / dine-in at a store. If they did not say, ask.
2. **Where.**
   - Delivery: list their addresses with `delivery-query-addresses` and let them pick one; only add one with `delivery-create-address` when they have none or ask for a new one, using exactly the details they gave. Then find the stores that deliver there with `delivery-query-stores`.
   - Pick-up: find stores near the place they name with `query-nearby-stores`, and let them pick.
3. **What.** Read the store's menu with `query-meals`, and a combo's contents and swappable items with `query-meal-detail`. Match what they asked for to real items and codes; if something is not on that store's menu, say so and offer the closest items.
4. **Coupons.** Check `query-store-coupons` for that store and point out ones that apply to what they chose.
5. **Price.** Always run `calculate-price` with the items (and any coupon) and show the user the breakdown it returns: items, delivery fee, discounts, total.
6. **Confirm, then order.** Show the store, the way they get it (and address or pick-up time), every item with options, the coupon, and the total, and wait for the user to say yes. Only then call `create-order`. It returns the order and a payment link: give them the link; they pay themselves. Never say an order is placed before `create-order` succeeded.

After ordering, `query-order` shows its status and delivery progress; `order-list` lists recent orders. Cancel with `cancel-order` only when the user asks to cancel that order.

## Coupons and points

- What they could claim: `available-coupons`. `auto-bind-coupons` claims every 麦麦省 coupon available at once: tell the user that is what it does before calling it.
- What they hold: `query-my-coupons`; points: `query-my-account`.
- Points mall: browse with `mall-points-products` and `mall-product-detail`; `mall-create-order` spends their points, so show the item and the points it costs and get a yes first. Mall orders are under `mall-order-list` / `mall-order-detail`, not `order-list`.
- Lottery: `query-lottery-info` shows the rules and prizes; `draw-lottery` spends points or chances each time, so draw only when asked, one draw per request unless they say how many. Prizes won: `query-my-prizes`.

## Other questions

- Calories and nutrition: `list-nutrition-foods`. For "a meal under N kcal", add up the real figures and show the sum.
- This month's campaigns: `campaign-calendar`.
- Themed events (parties, tastings): cities, stores, dates and sessions with the `query-party-*` tools, then `party-order-create` only after the user confirms the choice.

## Limits and errors

- Each token allows 600 calls a minute: do not loop over every store or every item when a few calls answer the question.
- If a call says the token is invalid or expired, stop and tell the user to give a new McDonald's token on the Plugins page; do not retry.
- If the user refuses an action, do not try another way to do the same thing.
