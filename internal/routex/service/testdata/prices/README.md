# Price workbook fixtures

These small BIFF8 workbooks were generated for RouteX using `xlwt` 1.3.0;
no third-party workbook contents or source code were copied. The writer is a
fixture-generation tool only and is not required by RouteX or its tests.

- `literal-biff8.xls`: the eight import headers, exact 18+18 digit decimal text,
  a native boolean, and numeric zero threshold.
- `formula-biff8.xls`: the same workbook with `1+1` in the amount cell.
- `numeric-amount-biff8.xls`: the amount is an IEEE floating-point cell, which
  must be rejected instead of importing its rounded value.
- `continued-sst-biff8.xls`: 80 rates for 20 models, with distinct Unicode display
  names that cross BIFF SST CONTINUE record boundaries.

The tests construct additional malformed, cyclic, truncated, oversized, and
unsupported variants in memory. OOXML samples are assembled as actual ZIP/XML
packages by the tests without requiring an office application.
