WITH
  "Part" AS (
   SELECT
     "Part"."partkey" "partkey"
   , "Part"."name" "name"
   FROM
     (
      SELECT
        "Part"."partkey" "partkey"
      , "Part"."name" "name"
      FROM
        (
         SELECT
           p_partkey "partkey"
         , p_name "name"
         FROM
           (
            SELECT *
            FROM
              tpch.part
         )  "Part"
      )  "Part"
   )  "Part"
) 
SELECT
  partkey
, name
FROM
  Part
