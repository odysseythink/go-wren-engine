WITH
  "orders" AS (
   SELECT
     "orders"."orderkey" "orderkey"
   , "orders"."custkey" "custkey"
   , "orders"."orderdate" "orderdate"
   , "orders"."totalprice" "totalprice"
   FROM
     (
      SELECT
        "orders"."orderkey" "orderkey"
      , "orders"."custkey" "custkey"
      , "orders"."orderdate" "orderdate"
      , "orders"."totalprice" "totalprice"
      FROM
        (
         SELECT
           "orderkey" "orderkey"
         , "custkey" "custkey"
         , "orderdate" "orderdate"
         , "totalprice" "totalprice"
         FROM
           "main"."orders" "orders"
      )  "orders"
   )  "orders"
) 
SELECT
  custkey
, count(*)
FROM
  orders
GROUP BY custkey
ORDER BY 1 DESC
