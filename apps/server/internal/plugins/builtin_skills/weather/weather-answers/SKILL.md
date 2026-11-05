---
name: weather-answers
description: How to answer weather questions with the weather plugin - current conditions, forecasts for today, tomorrow or the coming days, hour-by-hour rain, what to wear or whether to bring an umbrella, travel weather, and past weather on given dates.
---

# Answering weather questions

The weather plugin has `find_place`, `forecast` and `history`. All times it returns are the place's local time.

## Where

- Pass the place's name to `forecast` or `history` as `place`: the best-known place of that name is used, and the result says which (`place`). If that could be the wrong one (a common name, or a district of a city), check with `find_place` and pass its coordinates.
- If the user gave no place and none is clear from the conversation, ask for one rather than guessing.

## When

- Work out the dates from the current date of the conversation. "This weekend" and "tomorrow" are relative to the user's local time.
- Up to 16 days ahead use `forecast` (`days`). For rain timing or "this afternoon / tonight", set `hourly` for the next 48 hours.
- For past dates use `history` (from 1940 to a few days ago, up to 366 days at a time). For "what is the weather usually like in April", look at the same weeks of the last few years and say that is what you did.
- Forecasts beyond about a week are rough; say so when answering about them.

## Answering

- Lead with what the user asked: "Tomorrow in Shanghai: overcast, 14 to 19 °C, 70% chance of rain in the afternoon - take an umbrella."
- Give temperatures in °C (the plugin's unit). Add °F only if the user uses it.
- For several days, a short table (date, weather, high/low, rain chance) reads best.
- Translate the weather words into the user's language.
- Precipitation probability is a chance, not a promise. Combine it with the amount (mm) when advising: 30% chance of 0.2 mm is not worth an umbrella; 80% of 10 mm is.
- Do not add warnings or advice that the data does not support.
