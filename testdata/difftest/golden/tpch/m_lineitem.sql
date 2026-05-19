WITH
  "Lineitem" AS (
   SELECT
     "Lineitem"."orderkey" "orderkey"
   , "Lineitem"."partkey" "partkey"
   , "Lineitem"."linenumber" "linenumber"
   , "Lineitem"."extendedprice" "extendedprice"
   , "Lineitem"."discount" "discount"
   , "Lineitem"."shipdate" "shipdate"
   , "Lineitem"."comment" "comment"
   , "Lineitem"."order" "order"
   , "Lineitem"."orderkey_linenumber" "orderkey_linenumber"
   FROM
     (
      SELECT
        "Lineitem"."orderkey" "orderkey"
      , "Lineitem"."partkey" "partkey"
      , "Lineitem"."linenumber" "linenumber"
      , "Lineitem"."extendedprice" "extendedprice"
      , "Lineitem"."discount" "discount"
      , "Lineitem"."shipdate" "shipdate"
      , "Lineitem"."comment" "comment"
      , "Lineitem"."order" "order"
      , "Lineitem"."orderkey_linenumber" "orderkey_linenumber"
      FROM
        (
         SELECT
           l_orderkey "orderkey"
         , l_partkey "partkey"
         , l_linenumber "linenumber"
         , l_extendedprice "extendedprice"
         , l_discount "discount"
         , l_shipdate "shipdate"
         , l_comment "comment"
         , 1 "order"
         , concat(l_orderkey, l_linenumber) "orderkey_linenumber"
         FROM
           (
            SELECT *
            FROM
              tpch.lineitem
         )  "Lineitem"
      )  "Lineitem"
   )  "Lineitem"
) 
SELECT
  orderkey
, extendedprice
, discount
FROM
  Lineitem
