#!/usr/bin/env python3
"""
Fix all 'no new variables on left side of :=' errors in Go test files.

Strategy: In each Go function body, track which variables have been declared.
When we see `var := expr` and `var` is already declared in the same scope,
change `:=` to `=`.

Simplified approach: only fix lines where the sole new-looking var is `err`
and it's already been introduced by a previous `:=` in the same function.
"""
import re, glob

base = 'internal/infrastructure/postgres'
test_files = glob.glob(f'{base}/*_test.go')

total_fixes = 0

for fpath in sorted(test_files):
    with open(fpath) as f:
        lines = f.readlines()

    fixes = []
    # Track err declarations per function
    in_func = False
    err_declared = False
    func_start = 0

    for i, line in enumerate(lines):
        stripped = line.strip()

        # Detect function start
        if stripped.startswith('func ') and '{' in stripped:
            in_func = True
            err_declared = False
            func_start = i
            continue

        if not in_func:
            continue

        # Very rough brace tracking to detect function end
        # (not perfect but good enough for well-formatted Go test files)

        # Check if this line introduces err via :=
        if ':=' in line and 'err' in line:
            # Is this the first := that introduces err in this function?
            # Match patterns like: `err :=`, `foo, err :=`, `_, err :=`
            m = re.match(r'^(\s+)(.+?)\s*:=\s*(.+)$', line)
            if m:
                lhs = m.group(2).strip()
                vars_on_lhs = [v.strip() for v in lhs.split(',')]
                if 'err' in vars_on_lhs:
                    if not err_declared:
                        # First declaration of err in this function
                        err_declared = True
                    else:
                        # err already declared - check if there are new vars
                        new_vars = [v for v in vars_on_lhs if v not in ('err', '_')]
                        if not new_vars:
                            # All vars are err or _ - should use = instead of :=
                            fixes.append(i)

    if fixes:
        for i in fixes:
            old = lines[i]
            # Replace first := with =
            lines[i] = old.replace(':=', '=', 1)
            print(f'  {fpath}:{i+1}: {old.strip()} -> {lines[i].strip()}')
            total_fixes += 1
        with open(fpath, 'w') as f:
            f.writelines(lines)

print(f'Total fixes: {total_fixes}')
