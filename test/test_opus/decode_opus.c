#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <opus/opus.h>

// 24000Hz采样率、单声道、60ms帧长度对应的PCM样本数
#define SAMPLE_RATE 24000
#define CHANNELS 1
#define FRAME_SIZE_MS 60
#define FRAME_SIZE (SAMPLE_RATE * FRAME_SIZE_MS / 1000)

// 每帧最大字节数（安全值）
#define MAX_PACKET_SIZE 1500

int main(int argc, char *argv[]) {
    if (argc < 2) {
        printf("用法: %s <opus文件> [raw]\n", argv[0]);
        printf("参数说明:\n");
        printf("  <opus文件>: 要解码的opus文件路径\n");
        printf("  [raw]: 可选参数，指定为raw则处理无长度前缀的raw opus数据\n");
        return 1;
    }

    // 检查是否为raw模式
    int raw_mode = 1;

    // 打开opus文件
    FILE *fp = fopen(argv[1], "rb");
    if (!fp) {
        printf("无法打开文件: %s\n", argv[1]);
        return 1;
    }

    // 获取文件大小
    fseek(fp, 0, SEEK_END);
    long file_size = ftell(fp);
    fseek(fp, 0, SEEK_SET);

    // 读取整个文件内容
    unsigned char *opus_data = (unsigned char *)malloc(file_size);
    if (!opus_data) {
        printf("内存分配失败\n");
        fclose(fp);
        return 1;
    }
    
    size_t bytes_read = fread(opus_data, 1, file_size, fp);
    fclose(fp);
    
    printf("读取文件成功，大小: %ld 字节\n", bytes_read);

    // 创建opus解码器
    int error;
    OpusDecoder *decoder = opus_decoder_create(SAMPLE_RATE, CHANNELS, &error);
    if (error != OPUS_OK) {
        printf("创建opus解码器失败: %s\n", opus_strerror(error));
        free(opus_data);
        return 1;
    }
    
    printf("解码器创建成功，采样率: %d Hz, 声道数: %d\n", SAMPLE_RATE, CHANNELS);
    printf("理论每帧PCM样本数(60ms): %d\n", FRAME_SIZE);

    // 准备PCM输出缓冲区 - 理论上60ms@24000Hz应该有1440个样本点
    opus_int16 pcm[FRAME_SIZE * CHANNELS];

    int frame_count = 0;
    
    // 尝试将整个文件当作一个opus帧解码
    int samples = opus_decode(decoder, opus_data, bytes_read, pcm, FRAME_SIZE, 0);
    
    if (samples < 0) {
        printf("解码失败: %s\n", opus_strerror(samples));
    } else {
        frame_count++;
        printf("解码完成: opus长度 %ld 字节, 解码后PCM样本数 %d\n", bytes_read, samples);
        
        // 可以将PCM保存为文件
        char output_file[256];
        sprintf(output_file, "%s.pcm", argv[1]);
        FILE *out_fp = fopen(output_file, "wb");
        if (out_fp) {
            fwrite(pcm, sizeof(opus_int16), samples, out_fp);
            fclose(out_fp);
            printf("已保存PCM数据到 %s\n", output_file);
        }
    }
    
    printf("总共解码 %d 帧\n", frame_count);
    
    // 清理资源
    opus_decoder_destroy(decoder);
    free(opus_data);
    
    return 0;
}
