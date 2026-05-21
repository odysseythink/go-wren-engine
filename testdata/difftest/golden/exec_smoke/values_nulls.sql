SELECT *
FROM
  (
 VALUES 
     ROW (1, null)
   , ROW (null, 'b')
   , ROW (null, null)
)  v (a, b)
