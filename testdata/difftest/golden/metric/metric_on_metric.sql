WITH
  "Orders" AS (
   SELECT
     "Orders"."orderkey" "orderkey"
   , "Orders"."custkey" "custkey"
   , "Orders"."totalprice" "totalprice"
   , "Orders"."orderdate" "orderdate"
   FROM
     (
      SELECT
        "Orders"."orderkey" "orderkey"
      , "Orders"."custkey" "custkey"
      , "Orders"."totalprice" "totalprice"
      , "Orders"."orderdate" "orderdate"
      FROM
        (
         SELECT
           o_orderkey "orderkey"
         , o_custkey "custkey"
         , o_totalprice "totalprice"
         , o_orderdate "orderdate"
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
, "RevenueByCustomer" AS (
   SELECT
     custkey "custkey"
   , sum(totalprice) "totalprice"
   FROM
     Revenue
   GROUP BY 1
) 
SELECT
  custkey
, totalprice
FROM
  RevenueByCustomer
