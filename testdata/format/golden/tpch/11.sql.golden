SELECT
  ps_partkey
, sum((ps_supplycost * ps_availqty)) value1
FROM
  partsupp
, supplier
, nation
WHERE ((ps_suppkey = s_suppkey) AND (s_nationkey = n_nationkey) AND (n_name = 'GERMANY'))
GROUP BY ps_partkey
HAVING (sum((ps_supplycost * ps_availqty)) > (SELECT (sum((ps_supplycost * ps_availqty)) * 1E-4)
FROM
  partsupp
, supplier
, nation
WHERE ((ps_suppkey = s_suppkey) AND (s_nationkey = n_nationkey) AND (n_name = 'GERMANY'))
))
ORDER BY value1 DESC
