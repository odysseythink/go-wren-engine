WITH
  "customer" AS (
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
SELECT name
FROM
  customer
WHERE ((custkey = 1) AND (nationkey = 2))
