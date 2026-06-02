"""Frozen-app entrypoint for the Streamlit dashboard.

PyInstaller builds this into the `appie-dashboard` executable. The launcher
invokes it exactly like the `streamlit` CLI, e.g.:

    appie-dashboard run app.py --server.port=8501 ...

so we just hand argv straight to Streamlit's CLI. Running Streamlit in-process
(rather than re-execing a `streamlit` binary that doesn't exist in a frozen
bundle) is what makes the single-file approach work.
"""

import os
import sys


def main() -> int:
    # When frozen, PyInstaller (onedir, v6+) extracts the bundled app.py and
    # its sibling modules into the _internal directory, exposed at
    # sys._MEIPASS. Make that the working dir and resolve any script argument
    # (e.g. "app.py") to its absolute path there, so Streamlit can find it
    # regardless of where the launcher invoked us from.
    if getattr(sys, "frozen", False):
        # PyInstaller's onedir layout differs by version/OS: data files land
        # either in sys._MEIPASS or in an "_internal" dir next to the exe. Try
        # all plausible roots so a bare "app.py" argument always resolves —
        # otherwise Streamlit reports the entry point cannot be found (the
        # symptom seen on Windows). Order matters: _MEIPASS first, then the
        # _internal sibling, then the exe dir itself.
        exe_dir = os.path.dirname(sys.executable)
        roots = []
        meipass = getattr(sys, "_MEIPASS", None)
        if meipass:
            roots.append(meipass)
        roots.append(os.path.join(exe_dir, "_internal"))
        roots.append(exe_dir)

        bundle_dir = next((r for r in roots if os.path.isdir(r)), exe_dir)
        os.chdir(bundle_dir)
        if bundle_dir not in sys.path:
            sys.path.insert(0, bundle_dir)

        # Rewrite a bare script name in argv to its bundled absolute path,
        # searching every candidate root.
        # argv looks like: ["appie-dashboard", "run", "app.py", "--flag", ...].
        for i, arg in enumerate(sys.argv):
            if arg.endswith(".py") and not os.path.isabs(arg):
                for r in roots:
                    candidate = os.path.join(r, arg)
                    if os.path.exists(candidate):
                        sys.argv[i] = candidate
                        break

    # Streamlit's CLI is a click command; invoking it with our argv mirrors the
    # real `streamlit` entrypoint. sys.argv[0] is the program name; the rest
    # ("run", "<app.py>", "--server.port=...") are passed through unchanged.
    from streamlit.web.cli import main as st_main  # noqa: E402

    sys.exit(st_main())


if __name__ == "__main__":
    main()
