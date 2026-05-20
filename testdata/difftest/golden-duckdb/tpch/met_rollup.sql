WITH
  "Nation" AS (
   SELECT
     "Nation"."nationkey" "nationkey"
   , "Nation"."name" "name"
   , "Nation"."regionkey" "regionkey"
   , "Nation"."comment" "comment"
   FROM
     (
      SELECT
        "Nation"."nationkey" "nationkey"
      , "Nation"."name" "name"
      , "Nation"."regionkey" "regionkey"
      , "Nation"."comment" "comment"
      FROM
        (
         SELECT
           n_nationkey "nationkey"
         , n_name "name"
         , n_regionkey "regionkey"
         , n_comment "comment"
         FROM
           (
            SELECT *
            FROM
              tpch.nation
         )  "Nation"
      )  "Nation"
   )  "Nation"
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
, "Orders" AS (
   SELECT
     "Orders"."orderkey" "orderkey"
   , "Orders"."custkey" "custkey"
   , "Orders"."orderstatus" "orderstatus"
   , "Orders"."totalprice" "totalprice"
   , "Orders_relationsub"."nation_name" "nation_name"
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
   LEFT JOIN (
      SELECT
        "Orders"."orderkey"
      , "Nation"."name" "nation_name"
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
      LEFT JOIN "Customer" ON ("Orders"."custkey" = "Customer"."custkey")
      LEFT JOIN "Nation" ON ("Customer"."nationkey" = "Nation"."nationkey")
   )  "Orders_relationsub" ON ("Orders"."orderkey" = "Orders_relationsub"."orderkey")
) 
SELECT *
FROM
  (
   SELECT
     date_trunc('YEAR', orderdate) "orderdate"
   , customer.name "customer"
   , sum(totalprice) "totalprice"
   FROM
     "Orders"
   GROUP BY 1, 2
)  Revenue
