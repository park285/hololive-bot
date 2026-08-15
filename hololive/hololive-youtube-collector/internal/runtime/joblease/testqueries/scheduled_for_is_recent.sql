SELECT $1 >= clock_timestamp() - INTERVAL '1 minute'
   AND $1 <= clock_timestamp()
