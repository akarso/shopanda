#!/usr/bin/env python3
import re, glob

base = 'internal/infrastructure/postgres'
test_files = glob.glob(f'{base}/*_test.go')

constructors = [
    'NewProductRepo', 'NewVariantRepo', 'NewCartRepo', 'NewOrderRepo',
    'NewStockRepo', 'NewPriceRepo', 'NewCustomerRepo', 'NewReservationRepo',
    'NewPaymentRepo', 'NewShippingRepo', 'NewCategoryRepo', 'NewCollectionRepo',
    'NewResetTokenRepo', 'NewAssetRepo', 'NewCacheStore', 'NewJobQueue',
    'NewSearchEngine', 'NewTaxRateRepo',
]

total = 0
for fpath in sorted(test_files):
    with open(fpath) as f:
        content = f.read()

    original = content
    for ctor in constructors:
        pattern = r'(\t+)(\w+) := postgres\.' + ctor + r'\((\w+)\)'
        def make_replacer(cname):
            def replacer(m):
                indent = m.group(1)
                var = m.group(2)
                arg = m.group(3)
                return (f'{indent}{var}, err := postgres.{cname}({arg})\n'
                        f'{indent}if err != nil {{\n'
                        f'{indent}\tt.Fatalf("{cname}: %v", err)\n'
                        f'{indent}}}')
            return replacer
        content, n = re.subn(pattern, make_replacer(ctor), content)
        total += n

    if content != original:
        with open(fpath, 'w') as f:
            f.write(content)
        print(f'  Updated {fpath}')

print(f'Total replacements: {total}')
