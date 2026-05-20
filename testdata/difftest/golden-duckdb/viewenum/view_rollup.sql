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
, "useMetricRollUp" AS (
   SELECT *
   FROM
     (
      SELECT
        date_trunc('YEAR', orderdate) "orderdate"
      , custkey "custkey"
      , sum(totalprice) "totalprice"
      FROM
        "Orders"
      GROUP BY 1, 2
   )  Revenue
) 
SELECT *
FROM
  useMetricRollUp
