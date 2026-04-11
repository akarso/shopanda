#!/usr/bin/env python3
"""Fix 'no new variables on left side of :=' errors in test files.

In each test function, if err is first introduced by a New*Repo call,
subsequent standalone 'err :=' or '_, err :=' must become '=' instead of ':='.
"""
import re

fixes = {
    'internal/infrastructure/postgres/customer_repo_test.go': [153, 211],
    'internal/infrastructure/postgres/job_queue_test.go': [227],
    'internal/infrastructure/postgres/product_repo_test.go': [203, 261, 282],
    'internal/infrastructure/postgres/reservation_repo_test.go': [96, 159, 216, 278],
}

for fpath, lines in fixes.items():
    with open(fpath) as f:
        content = f.readlines()

    for lineno in lines:
        idx = lineno - 1  # 0-based
        line = content[idx]
        # Replace ':=' with '=' but only on lines that have 'err :='
        # and where err is already declared (from New*Repo earlier in the func)
        new_line = line.replace(':=', '=', 1)
        if new_line != line:
            content[idx] = new_line
            print(f'  {fpath}:{lineno}: {line.strip()} -> {new_line.strip()}')

    with open(fpath, 'w') as f:
        f.writelines(content)

print('Done')
