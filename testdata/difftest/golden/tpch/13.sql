SELECT
  c_count
, count(*) custdist
FROM
  (
   SELECT
     c_custkey
   , count(o_orderkey)
   FROM
     customer
   LEFT JOIN orders ON ((c_custkey = o_custkey) AND (NOT (o_comment LIKE '%special%requests%')))
   GROUP BY c_custkey
)  c_orders (c_custkey, c_count)
GROUP BY c_count
ORDER BY custdist DESC, c_count DESC
