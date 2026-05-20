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
SELECT
  nationkey
, name
FROM
  Nation
