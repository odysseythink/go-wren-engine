select l.orderkey, p.name
from Lineitem l join Part p on l.partkey = p.partkey
