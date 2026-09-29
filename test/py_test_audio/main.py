#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import asyncio
import json
import time
import argparse
import sys
import os
from datetime import datetime
import struct

# 检查websockets库的可用性
try:
    import websockets
except ImportError:
    print("警告: websockets未安装，请使用 'pip install websockets' 安装")
    sys.exit(1)

# 检查opus库的可用性
try:
    import opuslib
except ImportError:
    print("警告: opuslib未安装，请使用 'pip install opuslib' 安装")
    HAS_OPUS = False
else:
    HAS_OPUS = True

# 用于保存日志的辅助函数
def log(message):
    """记录日志到控制台和文件"""
    print(message)
    with open("websocket_client.log", "a", encoding="utf-8") as f:
        timestamp = time.strftime("%Y-%m-%d %H:%M:%S")
        f.write(f"[{timestamp}] {message}\n")

# 音频参数常量
AUDIO_RATE = 24000
FRAME_DURATION = 20

# Opus编码常量
SAMPLE_RATE = 16000
CHANNELS = 1
FRAME_DURATION_MS = 60
PCM_BUFFER_SIZE = SAMPLE_RATE * CHANNELS * FRAME_DURATION_MS // 1000

# 消息类型常量
MESSAGE_TYPE_HELLO = "hello"
MESSAGE_TYPE_LISTEN = "listen"
MESSAGE_TYPE_ABORT = "abort"
MESSAGE_TYPE_IOT = "iot"

# 消息状态常量
MESSAGE_STATE_START = "start"
MESSAGE_STATE_STOP = "stop"
MESSAGE_STATE_DETECT = "detect"
MESSAGE_STATE_SUCCESS = "success"
MESSAGE_STATE_ERROR = "error"
MESSAGE_STATE_ABORT = "abort"

# 全局变量
opus_data = []
detect_start_ts = 0

async def send_json_message(websocket, msg):
    """发送JSON消息"""
    data = json.dumps(msg)
    log(f"发送消息: {data}")
    await websocket.send(data)

async def send_listen_start(websocket, device_id):
    """发送listen start消息"""
    listen_start_msg = {
        "type": MESSAGE_TYPE_LISTEN,
        "device_id": device_id,
        "state": MESSAGE_STATE_START,
        "mode": "manual"
    }
    await send_json_message(websocket, listen_start_msg)

async def send_listen_stop(websocket, device_id):
    """发送listen stop消息"""
    listen_stop_msg = {
        "type": MESSAGE_TYPE_LISTEN,
        "device_id": device_id,
        "state": MESSAGE_STATE_STOP,
        "mode": "manual"
    }
    await send_json_message(websocket, listen_stop_msg)

async def send_listen_detect(websocket, device_id, text):
    """发送listen detect消息"""
    listen_detect_msg = {
        "type": MESSAGE_TYPE_LISTEN,
        "device_id": device_id,
        "state": MESSAGE_STATE_DETECT,
        "text": text
    }
    await send_json_message(websocket, listen_detect_msg)

def save_opus_data():
    """保存Opus数据到文件"""
    with open("opus_ws.data", "wb") as f:
        for data in opus_data:
            f.write(data)
    log(f"已保存{len(opus_data)}帧Opus数据到opus_ws.data")

def opus_to_wav(opus_data, sample_rate, channels, output_file):
    """将Opus数据转换为WAV文件
    
    步骤:
    1. 使用opuslib解码opus数据为PCM格式
    2. 使用wave库将PCM数据写入WAV文件
    """
    log(f"将{len(opus_data)}帧Opus数据转换为WAV文件: {output_file}")
    
    if not HAS_OPUS:
        log("opus解码器未安装，请安装opuslib: pip install opuslib")
        
        # 如果没有opus库，至少将原始数据保存起来
        raw_output = output_file + ".opus.raw"
        with open(raw_output, 'wb') as f:
            for data in opus_data:
                f.write(data)
        log(f"已将原始opus数据保存到: {raw_output}")
        log("要播放此音频，请先安装opuslib并再次运行程序")
        
        # 创建一个空的WAV文件，只包含头信息
        try:
            import wave
            # 生成1秒静音的WAV文件
            silence = b'\x00' * (sample_rate * channels * 2)
            with wave.open(output_file, 'wb') as wav_file:
                wav_file.setnchannels(channels)
                wav_file.setsampwidth(2)  # 16-bit PCM
                wav_file.setframerate(sample_rate)
                wav_file.writeframes(silence)
            log(f"已创建包含静音的WAV文件: {output_file}")
        except Exception as e:
            log(f"创建WAV文件失败: {e}")
        return

    try:
        import wave
        # 1. 创建opus解码器
        decoder = opuslib.Decoder(sample_rate, channels)
        
        # 将所有帧解码为PCM (16位小端序PCM)
        pcm_data = bytearray()
        
        # 解码每一帧
        total_input_bytes = 0
        log("\n====== 每帧解码详情 ======")
        log("帧号\t输入字节\t输出字节\t采样点数\t时长(ms)")
        log("----------------------------------------")
        
        for i, frame in enumerate(opus_data):
            try:
                # 计算最大PCM长度 = 采样率 * 通道数 * 最大帧持续时间(120ms) / 1000 * 2(字节/样本)
                max_pcm_size = int(sample_rate * channels * 120 / 1000 * 2)
                
                # 解码opus帧到PCM
                decoded_pcm = decoder.decode(frame, max_pcm_size)
                
                # 计算输出统计信息
                input_bytes = len(frame)
                total_input_bytes += input_bytes
                output_bytes = len(decoded_pcm)
                output_samples = output_bytes // (2 * channels)  # 16位PCM
                output_duration_ms = output_samples * 1000 / sample_rate
                
                pcm_data.extend(decoded_pcm)
                
                # 打印每帧信息
                log(f"{i}\t{input_bytes}\t\t{output_bytes}\t\t{output_samples}\t\t{output_duration_ms:.2f}")
                
            except Exception as e:
                log(f"解码第 {i} 帧失败: {e}")
        
        # 计算总体统计
        total_samples = len(pcm_data) // (2 * channels)
        total_duration_ms = total_samples * 1000 / sample_rate
        
        # 打印总体统计
        log("\n====== 总体统计 ======")
        log(f"总帧数: {len(opus_data)}")
        log(f"总输入字节: {total_input_bytes}")
        log(f"总输出字节: {len(pcm_data)}")
        log(f"总采样点数: {total_samples}")
        log(f"总时长: {total_duration_ms:.2f}ms ({total_duration_ms/1000:.2f}秒)")
        log("=======================\n")
        
        # 2. 将PCM数据写入WAV文件
        # WAV文件结构: RIFF头 + 格式块 + 数据块
        with wave.open(output_file, 'wb') as wav_file:
            wav_file.setnchannels(channels)         # 设置通道数
            wav_file.setsampwidth(2)                # 设置样本宽度为2字节 (16位)
            wav_file.setframerate(sample_rate)      # 设置采样率
            wav_file.writeframes(bytes(pcm_data))   # 写入PCM数据
        
        log(f"音频解码与转换完成，共 {len(pcm_data)} 字节")
        log(f"音频时长: {total_duration_ms/1000:.2f} 秒")
        log(f"音频文件已保存到: {output_file}")
        
    except Exception as e:
        log(f"转换过程中出错: {e}")
        import traceback
        traceback.print_exc()

async def send_text_to_speech(websocket, device_id, text):
    """调用tts服务生成语音并发送"""
    log(f"发送文本到语音: {text}")
    
    # 发送listen_detect消息
    try:
        await send_listen_detect(websocket, device_id, text)
        log("已发送listen_detect消息")
    except Exception as e:
        log(f"发送listen_detect消息失败: {e}")
    
    return

async def receive_messages(websocket):
    """处理从服务器接收的消息"""
    global opus_data
    first_recv_frame = False
    recv_interval = 0
    
    try:
        while True:
            try:
                message = await websocket.recv()
                
                if isinstance(message, str):
                    log(f"收到服务器消息: {message}")
                    try:
                        server_msg = json.loads(message)
                        
                        if server_msg.get("type") == "tts" and server_msg.get("state") == "stop":
                            opus_to_wav(opus_data, 24000, 1, "ws_output_24000.wav")
                    except json.JSONDecodeError:
                        log(f"无法解析JSON消息: {message}")
                
                elif isinstance(message, bytes):
                    now = int(time.time() * 1000)
                    if not first_recv_frame:
                        first_recv_frame = True
                        log(f"首帧到达时间: {now - detect_start_ts} 毫秒")
                    
                    opus_data.append(message)
                    log(f"收到音频数据: {len(message)} 字节, 间隔: {now - recv_interval} 毫秒")
                    recv_interval = now
            except Exception as e:
                log(f"处理消息时出错: {e}")
                import traceback
                traceback.print_exc()
                break
    
    except websockets.exceptions.ConnectionClosed:
        log("连接已关闭")
    except Exception as e:
        log(f"接收消息时出错: {e}")
        import traceback
        traceback.print_exc()

async def client(server_addr, device_id, audio_file, text):
    """运行WebSocket客户端"""
    global opus_data, detect_start_ts
    opus_data = []
    
    log(f"正在连接服务器: {server_addr}")
    
    try:
        # 设置HTTP头
        headers = {
            "Device-Id": device_id,
            "Content-Type": "application/json"
        }
        
        # Python 3.6需要不同的WebSocket连接方式
        async with websockets.connect(server_addr, extra_headers=headers) as websocket:
            log("已连接到服务器")
            
            # 发送hello消息
            hello_msg = {
                "type": MESSAGE_TYPE_HELLO,
                "device_id": device_id,
                "transport": "websocket",
                "version": 1,
                "audio_params": {
                    "sample_rate": SAMPLE_RATE,
                    "channels": CHANNELS,
                    "frame_duration": FRAME_DURATION_MS,
                    "format": "opus"
                }
            }
            
            await send_json_message(websocket, hello_msg)
            
            # 启动接收消息的任务 - Python 3.6兼容
            receive_task = asyncio.ensure_future(receive_messages(websocket))
            
            # 等待接收服务器响应
            await asyncio.sleep(1)
            
            log("开始发送音频数据...")
            detect_start_ts = int(time.time() * 1000)
            
            # 发送文本到语音
            try:
                await send_text_to_speech(websocket, device_id, text)
            except Exception as e:
                log(f"发送文本到语音失败: {e}")
                import traceback
                traceback.print_exc()
            
            # 等待接收消息的任务完成
            try:
                await asyncio.sleep(30)  # 等待30秒接收服务器响应
                receive_task.cancel()
            except asyncio.CancelledError:
                pass
            except Exception as e:
                log(f"等待接收消息时出错: {e}")
                import traceback
                traceback.print_exc()
    
    except Exception as e:
        log(f"客户端连接出错: {e}")
        import traceback
        traceback.print_exc()

def main():
    """主函数"""
    # 检测Python版本
    py_version = sys.version_info
    log(f"Python版本: {py_version.major}.{py_version.minor}.{py_version.micro}")
    
    # 创建日志文件
    with open("websocket_client.log", "w", encoding="utf-8") as f:
        f.write(f"=== 小智WebSocket客户端日志 - {time.strftime('%Y-%m-%d %H:%M:%S')} ===\n")
    
    parser = argparse.ArgumentParser(description="小智WebSocket客户端")
    parser.add_argument("--server", "-s", default="ws://localhost:8989/xiaozhi/v1/", help="服务器地址")
    parser.add_argument("--device", "-d", default="test-device-001", help="设备ID")
    parser.add_argument("--audio", "-a", default="../test.wav", help="音频文件路径")
    parser.add_argument("--text", "-t", default="你好测试", help="文本")
    
    args = parser.parse_args()
    
    log(f"运行小智客户端\n服务器: {args.server}\n设备ID: {args.device}\n音频文件: {args.audio}\n文本: {args.text}")
    
    try:
        # 兼容不同Python版本
        loop = asyncio.get_event_loop()
        loop.run_until_complete(client(args.server, args.device, args.audio, args.text))
        loop.close()
    except Exception as e:
        log(f"客户端运行失败: {e}")
        import traceback
        traceback.print_exc()

if __name__ == "__main__":
    main()
