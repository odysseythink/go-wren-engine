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
, "customer" AS (
   SELECT
     "customer"."custkey" "custkey"
   , "customer"."name" "name"
   , "customer"."nationkey" "nationkey"
   FROM
     (
      SELECT
        "customer"."custkey" "custkey"
      , "customer"."name" "name"
      , "customer"."nationkey" "nationkey"
      FROM
        (
         SELECT
           "custkey" "custkey"
         , "name" "name"
         , "nationkey" "nationkey"
         FROM
           "main"."customer" "customer"
      )  "customer"
   )  "customer"
) 
SELECT
  c.name
, o.orderkey
FROM
  customer c
INNER JOIN orders o ON (c.custkey = o.custkey)
