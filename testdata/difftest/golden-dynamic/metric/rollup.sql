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
SELECT *
FROM
  (
   SELECT
     DATE_TRUNC('YEAR', orderdate) "orderdate"
   , custkey "custkey"
   , sum(totalprice) "totalprice"
   FROM
     "Orders"
   GROUP BY 1, 2
)  Revenue
