---
name: luckin-ordering
description: How to order Luckin Coffee (瑞幸) with the Luckin plugin - finding a store near the user, finding drinks and choosing their options (temperature, sweetness, size), the price after discounts, placing a pick-up order and handing over its payment code, and checking or cancelling an order. Use whenever the user mentions Luckin / 瑞幸 / 瑞幸咖啡, wants to order coffee or a drink to pick up in mainland China, or asks about a Luckin order, store, price or menu.
---

# Luckin Coffee

The Luckin plugin acts on the user's own Luckin account. It serves mainland China only, and orders are picked up at a store. Its tools are `queryShopList`, `searchProductForMcp`, `queryProductDetailInfo`, `switchProduct`, `previewOrder`, `createOrder`, `queryOrderDetailInfo` and `cancelOrder` (call them as `tools.luckin.queryShopList({ ... })`). Read a tool's signature with `search("previewOrder", { plugin: "luckin" })` when you are not sure of its input.

## Never guess

Stores, menus, options and prices differ by store and change. Do not name a drink, an option or a price from memory: read them from the tools for the store the user will order from, and quote the service's own figures. Never pass example ids, SKU codes or attribute ids: use only ones a tool returned in this conversation.

## Ordering, step by step

Ask the user at each choice rather than choosing for them.

1. **Where they are.** `queryShopList` needs a longitude and latitude. Never guess the user's location: ask where they are, or which store or place they want to pick up from. Turn the place they name into coordinates with a tool that can (a place search such as the weather plugin's, or the web), and say which place you used. Add `deptName` when they named a store.
2. **Which store.** Show a few nearby stores (name, address, distance, whether open) and let them pick one. Use its `deptId` from then on.
3. **Which drink.** Search that store with `searchProductForMcp`, passing the user's own words as `query`. If nothing fits, try other words, then say so and offer what is close.
4. **Options.** Read the drink with `queryProductDetailInfo`: its option groups (temperature, sweetness, size, extras) and their values. Ask about the choices that matter instead of assuming defaults. Apply each choice with `switchProduct`, using the attribute and value ids from the latest detail, `operation: 3` to select, and the quantity; each call returns the SKU code to use next, so always use the one from the last call.
5. **Price.** Run `previewOrder` with the store, and each item's `productId`, final `skuCode` and amount. Show the user the result: every item with its options, the original total, the discount and the amount to pay. It may apply the user's coupons on its own; mention them if it did.
6. **Confirm, then order.** Show the store, each item with options and quantity, and the amount to pay, and wait for the user to say yes. Only then call `createOrder` with exactly what was previewed: the same items and SKUs, the store's coordinates, and `couponCodeList` from the preview. If anything changed since the preview, preview again first. Never say an order is placed before `createOrder` succeeded.
7. **Payment.** The order comes back unpaid with a payment QR code link (`payOrderQrCodeUrl`): give it to the user to scan and pay themselves. You cannot pay. "Order placed" is not "paid".

## After ordering

- `queryOrderDetailInfo` with the order id shows its state and, once it is being made or ready, the pick-up code. Do not tell the user it is paid or ready unless the order says so.
- `cancelOrder` only when the user asks to cancel that order.
- If `createOrder` fails or times out, do not simply try again: an order may have been made anyway. Ask the user to check their Luckin app first.

## Errors

- If a call says the token is invalid or has expired (访问令牌无效或已过期), stop and tell the user to give a new Luckin token on the Plugins page; do not retry. Luckin tokens run out after a while.
- If the user refuses an action, do not try another way to do the same thing.
