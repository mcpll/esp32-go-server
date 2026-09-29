#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys
import os
import wave
import struct
import opuslib

def decode_raw_opus(opus_data, sample_rate=24000, channels=1, frame_size_ms=60):
    """解码原始Opus数据，返回PCM数据"""
    # 计算一个帧的样本数
    frame_size = int(sample_rate * frame_size_ms / 1000)
    
    # 创建解码器
    decoder = opuslib.Decoder(sample_rate, channels)
    
    # 尝试直接解码整个文件
    try:
        pcm_data = bytearray()
        decoded = decoder.decode(opus_data, frame_size, False)
        for sample in decoded:
            pcm_data.extend(struct.pack('<h', sample))
        return pcm_data
    except Exception as e:
        print(f"直接解码失败: {e}")
        return None

def main():
    # 检查命令行参数
    if len(sys.argv) < 2:
        print("用法: python dec_opus.py <opus_file>")
        return

    opus_file = sys.argv[1]
    
    # 检查文件是否存在
    if not os.path.exists(opus_file):
        print(f"错误: 文件 '{opus_file}' 不存在")
        return
    
    # 初始化参数
    sample_rate = 24000  # 采样率24000Hz
    channels = 1         # 单声道
    frame_size_ms = 60   # 帧大小60ms
    
    # 读取opus文件全部内容
    with open(opus_file, 'rb') as f:
        opus_data = f.read()
    
    print(f"读取原始Opus数据: {len(opus_data)} 字节")
    
    # 解码数据
    pcm_data = decode_raw_opus(opus_data, sample_rate, channels, frame_size_ms)
    
    if pcm_data is None or len(pcm_data) == 0:
        print("解码失败，未能生成PCM数据")
        return
    
    # 计算PCM数据长度（样本数）
    pcm_samples_count = len(pcm_data) // 2  # 每个样本2字节
    pcm_duration_ms = pcm_samples_count * 1000 / sample_rate
    
    print(f"解码后PCM数据大小: {len(pcm_data)} 字节")
    print(f"PCM样本数: {pcm_samples_count}")
    print(f"PCM时长: {pcm_duration_ms:.2f} 毫秒")

if __name__ == "__main__":
    main()
