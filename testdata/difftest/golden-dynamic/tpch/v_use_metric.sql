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
, "Customer" AS (
   SELECT
     "Customer"."custkey" "custkey"
   , "Customer"."nationkey" "nationkey"
   , "Customer"."name" "name"
   , "Customer"."custkey_name" "custkey_name"
   , "Customer"."custkey_call_concat" "custkey_call_concat"
   FROM
     (
      SELECT
        "Customer"."custkey" "custkey"
      , "Customer"."nationkey" "nationkey"
      , "Customer"."name" "name"
      , "Customer"."custkey_name" "custkey_name"
      , "Customer"."custkey_call_concat" "custkey_call_concat"
      FROM
        (
         SELECT
           c_custkey "custkey"
         , c_nationkey "nationkey"
         , c_name "name"
         , concat(c_custkey, c_name) "custkey_name"
         , concat(c_custkey, c_custkey) "custkey_call_concat"
         FROM
           (
            SELECT *
            FROM
              tpch.customer
         )  "Customer"
      )  "Customer"
   )  "Customer"
) 
, "Revenue" AS (
   SELECT
     "Orders_relationsub"."name" "customer"
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
   LEFT JOIN (
      SELECT
        "Orders"."orderkey"
      , "Customer"."name"
      FROM
        "Orders"
      LEFT JOIN "Customer" ON ("Orders"."custkey" = "Customer"."custkey")
   )  "Orders_relationsub" ON ("Orders"."orderkey" = "Orders_relationsub"."orderkey")
   GROUP BY 1
) 
, "useMetric" AS (
   SELECT *
   FROM
     Revenue
) 
SELECT *
FROM
  useMetric
