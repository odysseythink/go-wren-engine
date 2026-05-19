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
, "date_spine" AS (
   SELECT *
   FROM
     UNNEST(GENERATE_TIMESTAMP_ARRAY(TIMESTAMP '1970-01-01', TIMESTAMP '2077-12-31', INTERVAL  '1' DAY)) t (metric_time)
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
, "WeeklyRevenue" AS (
   SELECT
     metric_time orderdate
   , sum(DISTINCT measure_field) totalprice
   FROM
     (
      SELECT
        date_trunc('WEEK', d.metric_time) metric_time
      , measure_field
      FROM
        (
         SELECT CAST(metric_time AS date) metric_time
         FROM
           "date_spine"
      )  d
      LEFT JOIN (
         SELECT
           measure_field
         , metric_time
         FROM
           (
            SELECT
              totalprice measure_field
            , orderdate metric_time
            FROM
              Orders
         )  sub1
         WHERE ((metric_time >= CAST('1993-01-01' AS date)) AND (metric_time <= CAST('1993-12-31' AS date)))
      )  sub2 ON ((sub2.metric_time <= d.metric_time) AND (sub2.metric_time > (d.metric_time - INTERVAL  '7' DAY)))
      WHERE ((d.metric_time >= CAST('1993-01-01' AS date)) AND (d.metric_time <= CAST('1993-12-31' AS date)))
   )  sub3
   GROUP BY 1
   ORDER BY 1 ASC
) 
SELECT
  orderdate
, totalprice
FROM
  WeeklyRevenue
