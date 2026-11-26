# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Rainer Sestig und Mitwirkende.

# Prüfskript für Code-Aufgaben; läuft in der Sandbox (bwrap, ohne Netz).
# Aufruf: python3 -I harness.py test|visible
# Exit 0 = bestanden, 1 = nicht bestanden, 2 = Funktion fehlt, 3 = keine Beispiele.
import ast
import doctest
import json
import sys

task = json.load(open("task.json"))
glob = {"__name__": "solution"}
exec(compile(open("solution.py").read(), "solution.py", "exec"), glob)
fn = glob.get(task["entry_point"])
if not callable(fn):
    print("funktion fehlt:", task["entry_point"])
    sys.exit(2)

if sys.argv[1] == "test":
    tglob = dict(glob)
    exec(compile(task["tests"], "tests.py", "exec"), tglob)
    tglob["check"](fn)
    sys.exit(0)

# visible: nur die Beispiele aus dem Docstring der Aufgabe
doc = ""
for node in ast.walk(ast.parse(task["code_prompt"])):
    if isinstance(node, ast.FunctionDef) and node.name == task["entry_point"]:
        doc = ast.get_docstring(node) or ""
examples = doctest.DocTestParser().get_examples(doc)
if not examples:
    sys.exit(3)
runner = doctest.DocTestRunner(verbose=False)
runner.run(doctest.DocTest(examples, glob, "visible", None, 0, None), out=lambda s: None)
sys.exit(0 if runner.failures == 0 else 1)
