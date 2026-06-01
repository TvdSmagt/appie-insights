"""Chart configuration and constants."""

PLOTLY_TEMPLATE = "plotly_dark"

AH_COLORS = ["#0072CE", "#4A9EE8", "#7DBFFF", "#A8D5FF", "#CCE8FF", "#E0F3FF"]

# One per CO2eq food category — colorblind accessible
CATEGORY_COLORS: dict[str, str] = {
    "Dranken": "#7EA8BE",
    "Granen": "#C9A84C",
    "Plantaardig": "#8FAF7E",
    "Sauzen": "#9E8E7E",
    "Snoep": "#C48C8C",
    "Vis": "#6A9E9E",
    "Vlees": "#C17B5C",
    "Zuivel": "#D4C5A9",
}

CATEGORY_ICONS: dict[str, str] = {
    "Dranken": "🥤",
    "Granen": "🌾",
    "Plantaardig": "🥬",
    "Sauzen": "🫙",
    "Snoep": "🍬",
    "Vis": "🐟",
    "Vlees": "🥩",
    "Zuivel": "🧀",
}

NUTRISCORE_COLORS: dict[str, str] = {
    "A": "#038141",
    "B": "#85BB2F",
    "C": "#FECB02",
    "D": "#EE8100",
    "E": "#E63312",
}

SOURCE_COLORS: dict[str, str] = {
    "receipt": "#0072CE",
    "order": "#F5A623",
}

# Semantic chart colors used across pages
CHART_COLORS: dict[str, str] = {
    "spent": "#0072CE",       # AH blue — primary spend metric
    "discount": "#00A878",    # green — savings/discount
    "moving_avg": "#FF6B35",  # orange — trend line
    "co2eq": "#D97706",       # amber — CO₂eq series
    "cost": "#00AE4D",        # green — cost in cost/CO₂ comparison chart
    "unknown": "#888888",     # gray — missing/unknown values
}

# Reference: kg CO₂eq per person per month
# Dutch avg: ~1,050 kg/year/person (CBS/RIVM)
REF_AVG_KG_PER_PERSON_MONTH = 88.0
# Sustainable: ~500 kg/year/person (EAT-Lancet)
REF_SUSTAINABLE_KG_PER_PERSON_MONTH = 42.0
