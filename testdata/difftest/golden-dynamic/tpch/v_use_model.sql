WITH
  "Orders" AS (
   SELECT
     "Orders"."orderkey" "orderkey"
   , "Orders"."custkey" "custkey"
   , "Orders"."orderstatus" "orderstatus"
   , "Orders"."totalprice" "totalprice"
   , "Orders"."orderdate" "orderdate"
   FROM
     (
      SELECT
        "Orders"."orderkey" "orderkey"
      , "Orders"."custkey" "custkey"
      , "Orders"."orderstatus" "orderstatus"
      , "Orders"."totalprice" "totalprice"
      , "Orders"."orderdate" "orderdate"
      FROM
        (
         SELECT
           o_orderkey "orderkey"
         , o_custkey "custkey"
         , o_orderstatus "orderstatus"
         , o_totalprice "totalprice"
         , o_orderdate "orderdate"
         FROM
           (
            SELECT *
            FROM
              tpch.orders
         )  "Orders"
      )  "Orders"
   )  "Orders"
) 
, "useModel" AS (
   SELECT *
   FROM
     Orders
) 
SELECT *
FROM
  useModel
