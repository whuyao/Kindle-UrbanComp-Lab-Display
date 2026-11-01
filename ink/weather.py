"""Open-Meteo forecast grid near Guanggu Square; not a street weather station."""
import json
import math
from datetime import datetime, timezone, timedelta
from urllib.request import Request, urlopen

URL = ('https://api.open-meteo.com/v1/forecast?latitude=30.51&longitude=114.40'
       '&current=temperature_2m,weather_code&daily=temperature_2m_max,temperature_2m_min'
       '&timezone=Asia%2FShanghai&forecast_days=1')
TZ = timezone(timedelta(hours=8))
CONDITIONS = {0:'晴',1:'晴间多云',2:'多云',3:'阴',45:'雾',48:'雾',
              51:'小毛毛雨',53:'毛毛雨',55:'毛毛雨',56:'冻雨',57:'冻雨',
              61:'小雨',63:'中雨',65:'大雨',66:'冻雨',67:'冻雨',
              71:'小雪',73:'中雪',75:'大雪',77:'米雪',80:'阵雨',81:'阵雨',82:'强阵雨',
              85:'阵雪',86:'强阵雪',95:'雷雨',96:'雷雨伴冰雹',99:'雷雨伴冰雹'}


def parse_weather(raw, now=None):
    now = now or datetime.now(TZ)
    current = raw['current']; daily = raw['daily']
    stamp = datetime.fromisoformat(current['time']).replace(tzinfo=TZ)
    if abs((now - stamp).total_seconds()) > 10800: raise ValueError('WEATHER_STALE')
    values = [current['temperature_2m'],daily['temperature_2m_min'][0],daily['temperature_2m_max'][0]]
    if not all(isinstance(v,(float,int)) and math.isfinite(v) and -80<v<70 for v in values):
        raise ValueError('WEATHER_VALUES_INVALID')
    code = current['weather_code']
    if code not in CONDITIONS: raise ValueError('WEATHER_CODE_UNKNOWN')
    if daily['time'][0] != stamp.date().isoformat(): raise ValueError('WEATHER_DATE_MISMATCH')
    return {'place':'武汉光谷', 'condition':CONDITIONS[code], 'temperature':round(values[0]),
            'low':round(values[1]), 'high':round(values[2]), 'time':stamp.isoformat(),
            'provider':'Open-Meteo', 'source_url':'https://open-meteo.com/', 'stale':False}


def get_weather():
    with urlopen(Request(URL,headers={'User-Agent':'UrbanCompInk/0.2'}), timeout=12) as r:
        data=r.read(100001)
    if len(data)>100000: raise ValueError('WEATHER_TOO_LARGE')
    return parse_weather(json.loads(data))
