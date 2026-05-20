WITH
  "Orders" AS (
   SELECT
     "Orders"."orderkey" "orderkey"
   , "Orders"."custkey" "custkey"
   , "Orders"."totalprice" "totalprice"
   , "Orders"."orderdate" "orderdate"
   , "Orders"."orderstatus" "orderstatus"
   FROM
     (
      SELECT
        "Orders"."orderkey" "orderkey"
      , "Orders"."custkey" "custkey"
      , "Orders"."totalprice" "totalprice"
      , "Orders"."orderdate" "orderdate"
      , "Orders"."orderstatus" "orderstatus"
      FROM
        (
         SELECT
           o_orderkey "orderkey"
         , o_custkey "custkey"
         , o_totalprice "totalprice"
         , o_orderdate "orderdate"
         , o_orderstatus "orderstatus"
         FROM
           (
            SELECT *
            FROM
              orders
         )  "Orders"
      )  "Orders"
   )  "Orders"
) 
, "Revenue" AS (
   SELECT
     "Orders"."custkey" "custkey"
   , sum("Orders"."totalprice") "totalprice"
   FROM
     (
      SELECT *
      FROM
        (
         SELECT *
         FROM
           "Orders"
      )  "Orders"
   )  "Orders"
   GROUP BY 1
) 
, "useMetric" AS (
   SELECT *
   FROM
     Revenue
) 
SELECT
  custkey
, totalprice
FROM
  useMetric
