import copy
import unittest
from datetime import datetime
from ink.weather import parse_weather, TZ


class WeatherTests(unittest.TestCase):
    def setUp(self):
        self.now=datetime(2026,10,25,0,20,tzinfo=TZ)
        self.data={'current':{'time':'2026-10-25T00:15','temperature_2m':18.2,'weather_code':1},
                   'daily':{'time':['2026-10-25'],'temperature_2m_min':[15.1],'temperature_2m_max':[24.2]}}

    def test_current_weather_and_date(self):
        result=parse_weather(self.data,self.now)
        self.assertEqual(result['temperature'],18)
        self.assertEqual(result['condition'],'晴间多云')
        self.assertEqual(result['time'],'2026-10-25T00:15:00+08:00')

    def test_stale_and_mismatched_forecast_rejected(self):
        self.data['current']['time']='2026-10-24T08:00'
        with self.assertRaises(ValueError):parse_weather(self.data,self.now)
        self.data['current']['time']='2026-10-25T00:15'
        self.data['daily']['time']=['2026-10-24']
        with self.assertRaises(ValueError):parse_weather(self.data,self.now)

    def test_missing_or_implausible_temperature(self):
        for value in (None,float('nan'),90):
            data=copy.deepcopy(self.data);data['current']['temperature_2m']=value
            with self.assertRaises(ValueError):parse_weather(data,self.now)
