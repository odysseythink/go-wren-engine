SELECT (sum(l_extendedprice) / 7E0) avg_yearly
FROM
  lineitem
, part
WHERE ((p_partkey = l_partkey) AND (p_brand = 'Brand#23') AND (p_container = 'MED BOX') AND (l_quantity < (SELECT (2E-1 * avg(l_quantity))
FROM
  lineitem
WHERE (l_partkey = p_partkey)
)))
