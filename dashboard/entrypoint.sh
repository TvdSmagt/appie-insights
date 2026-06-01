#!/bin/sh
# Write the correct theme config before Streamlit starts, so it reads the
# user's saved preference instead of defaulting to dark on every cold start.
PYTHONPATH=/app python3 -c "from theme import init_theme; init_theme()"
exec streamlit run app.py --server.address=0.0.0.0 --server.port=8501
