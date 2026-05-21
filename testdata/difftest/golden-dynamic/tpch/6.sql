SELECT sum((l_extendedprice * l_discount)) revenue
FROM
  lineitem
WHERE ((l_shipdate >= date '1994-01-01') AND (l_shipdate < (date '1995-01-01' + INTERVAL  '1' YEAR)) AND (l_discount BETWEEN (6E-2 - 1E-2) AND (6E-2 + 1E-2)) AND (l_quantity < 24))
