使用redis来存储用户配置数据结构

#### 一. 配置
##### 1. 全局配置hget结构
xiaozhi:global:config

##### 2. 用户配置可以覆盖配置文件中的，hget结构
```
xiaozhi:userconfig:{deviceid}
    "llm": {
        "provider": "deepseek",         //与 配置文件 llm中的key对应
    },
    "tts": {
        "provider": "cosyvoice",        //与 配置文件 tts中的key对应
    }
```

#### 二. prompt
##### 1. 系统prompt get/set
>xiaozhi:llm:system:{deviceid}

##### 2. 聊天session prompt记录 sorted set结构
>xiaozhi:llm:{deviceid}
