# The snippets of the tests of internal/rvm, one for each label insn_N.
# RVM_REBUILD_GUEST=1 go test ./internal/rvm/ writes this file. Do not edit it.
.section .text.insn,"ax",@progbits
.option norelax
.globl _start
_start:
.globl insn_0
insn_0:
.option push
.option norvc
    add a0, a1, a2
.option pop
.globl insn_1
insn_1:
.option push
.option norvc
    sub a0, a1, a2
.option pop
.globl insn_2
insn_2:
.option push
.option norvc
    sll a0, a1, a2
.option pop
.globl insn_3
insn_3:
.option push
.option norvc
    slt a0, a1, a2
.option pop
.globl insn_4
insn_4:
.option push
.option norvc
    sltu a0, a1, a2
.option pop
.globl insn_5
insn_5:
.option push
.option norvc
    xor a0, a1, a2
.option pop
.globl insn_6
insn_6:
.option push
.option norvc
    srl a0, a1, a2
.option pop
.globl insn_7
insn_7:
.option push
.option norvc
    sra a0, a1, a2
.option pop
.globl insn_8
insn_8:
.option push
.option norvc
    or a0, a1, a2
.option pop
.globl insn_9
insn_9:
.option push
.option norvc
    and a0, a1, a2
.option pop
.globl insn_10
insn_10:
.option push
.option norvc
    mul a0, a1, a2
.option pop
.globl insn_11
insn_11:
.option push
.option norvc
    mulh a0, a1, a2
.option pop
.globl insn_12
insn_12:
.option push
.option norvc
    mulhsu a0, a1, a2
.option pop
.globl insn_13
insn_13:
.option push
.option norvc
    mulhu a0, a1, a2
.option pop
.globl insn_14
insn_14:
.option push
.option norvc
    div a0, a1, a2
.option pop
.globl insn_15
insn_15:
.option push
.option norvc
    divu a0, a1, a2
.option pop
.globl insn_16
insn_16:
.option push
.option norvc
    rem a0, a1, a2
.option pop
.globl insn_17
insn_17:
.option push
.option norvc
    remu a0, a1, a2
.option pop
.globl insn_18
insn_18:
.option push
.option norvc
    add a0, a0, a0
.option pop
.globl insn_19
insn_19:
.option push
.option norvc
    sub a0, a0, a0
.option pop
.globl insn_20
insn_20:
.option push
.option norvc
    mul a1, a1, a2
.option pop
.globl insn_21
insn_21:
.option push
.option norvc
    div a2, a1, a2
.option pop
.globl insn_22
insn_22:
.option push
.option norvc
    add a0, zero, a1
.option pop
.globl insn_23
insn_23:
.option push
.option norvc
    sub a0, zero, a1
.option pop
.globl insn_24
insn_24:
.option push
.option norvc
    add zero, a1, a2
.option pop
.globl insn_25
insn_25:
.option push
.option norvc
    mulhu zero, a1, a2
.option pop
.globl insn_26
insn_26:
.option push
.option norvc
    addi zero, a0, 5
.option pop
.globl insn_27
insn_27:
.option push
.option norvc
    lui zero, 0x12345
.option pop
.globl insn_28
insn_28:
.option push
.option norvc
    auipc zero, 1
.option pop
.globl insn_29
insn_29:
.option push
.option norvc
    add t6, t5, t4
.option pop
.globl insn_30
insn_30:
.option push
.option norvc
    add ra, sp, gp
.option pop
.globl insn_31
insn_31:
.option push
.option norvc
    addi a0, a1, 0
.option pop
.globl insn_32
insn_32:
.option push
.option norvc
    addi a0, a1, 1
.option pop
.globl insn_33
insn_33:
.option push
.option norvc
    addi a0, a1, 2
.option pop
.globl insn_34
insn_34:
.option push
.option norvc
    addi a0, a1, 4
.option pop
.globl insn_35
insn_35:
.option push
.option norvc
    addi a0, a1, 8
.option pop
.globl insn_36
insn_36:
.option push
.option norvc
    addi a0, a1, 16
.option pop
.globl insn_37
insn_37:
.option push
.option norvc
    addi a0, a1, 32
.option pop
.globl insn_38
insn_38:
.option push
.option norvc
    addi a0, a1, 64
.option pop
.globl insn_39
insn_39:
.option push
.option norvc
    addi a0, a1, 128
.option pop
.globl insn_40
insn_40:
.option push
.option norvc
    addi a0, a1, 256
.option pop
.globl insn_41
insn_41:
.option push
.option norvc
    addi a0, a1, 512
.option pop
.globl insn_42
insn_42:
.option push
.option norvc
    addi a0, a1, 1024
.option pop
.globl insn_43
insn_43:
.option push
.option norvc
    addi a0, a1, 2047
.option pop
.globl insn_44
insn_44:
.option push
.option norvc
    addi a0, a1, -1
.option pop
.globl insn_45
insn_45:
.option push
.option norvc
    addi a0, a1, -2
.option pop
.globl insn_46
insn_46:
.option push
.option norvc
    addi a0, a1, -1024
.option pop
.globl insn_47
insn_47:
.option push
.option norvc
    addi a0, a1, -2048
.option pop
.globl insn_48
insn_48:
.option push
.option norvc
    addi a0, a1, 1365
.option pop
.globl insn_49
insn_49:
.option push
.option norvc
    addi a0, a1, -1366
.option pop
.globl insn_50
insn_50:
.option push
.option norvc
    slti a0, a1, 0
.option pop
.globl insn_51
insn_51:
.option push
.option norvc
    slti a0, a1, 1
.option pop
.globl insn_52
insn_52:
.option push
.option norvc
    slti a0, a1, 2
.option pop
.globl insn_53
insn_53:
.option push
.option norvc
    slti a0, a1, 4
.option pop
.globl insn_54
insn_54:
.option push
.option norvc
    slti a0, a1, 8
.option pop
.globl insn_55
insn_55:
.option push
.option norvc
    slti a0, a1, 16
.option pop
.globl insn_56
insn_56:
.option push
.option norvc
    slti a0, a1, 32
.option pop
.globl insn_57
insn_57:
.option push
.option norvc
    slti a0, a1, 64
.option pop
.globl insn_58
insn_58:
.option push
.option norvc
    slti a0, a1, 128
.option pop
.globl insn_59
insn_59:
.option push
.option norvc
    slti a0, a1, 256
.option pop
.globl insn_60
insn_60:
.option push
.option norvc
    slti a0, a1, 512
.option pop
.globl insn_61
insn_61:
.option push
.option norvc
    slti a0, a1, 1024
.option pop
.globl insn_62
insn_62:
.option push
.option norvc
    slti a0, a1, 2047
.option pop
.globl insn_63
insn_63:
.option push
.option norvc
    slti a0, a1, -1
.option pop
.globl insn_64
insn_64:
.option push
.option norvc
    slti a0, a1, -2
.option pop
.globl insn_65
insn_65:
.option push
.option norvc
    slti a0, a1, -1024
.option pop
.globl insn_66
insn_66:
.option push
.option norvc
    slti a0, a1, -2048
.option pop
.globl insn_67
insn_67:
.option push
.option norvc
    slti a0, a1, 1365
.option pop
.globl insn_68
insn_68:
.option push
.option norvc
    slti a0, a1, -1366
.option pop
.globl insn_69
insn_69:
.option push
.option norvc
    sltiu a0, a1, 0
.option pop
.globl insn_70
insn_70:
.option push
.option norvc
    sltiu a0, a1, 1
.option pop
.globl insn_71
insn_71:
.option push
.option norvc
    sltiu a0, a1, 2
.option pop
.globl insn_72
insn_72:
.option push
.option norvc
    sltiu a0, a1, 4
.option pop
.globl insn_73
insn_73:
.option push
.option norvc
    sltiu a0, a1, 8
.option pop
.globl insn_74
insn_74:
.option push
.option norvc
    sltiu a0, a1, 16
.option pop
.globl insn_75
insn_75:
.option push
.option norvc
    sltiu a0, a1, 32
.option pop
.globl insn_76
insn_76:
.option push
.option norvc
    sltiu a0, a1, 64
.option pop
.globl insn_77
insn_77:
.option push
.option norvc
    sltiu a0, a1, 128
.option pop
.globl insn_78
insn_78:
.option push
.option norvc
    sltiu a0, a1, 256
.option pop
.globl insn_79
insn_79:
.option push
.option norvc
    sltiu a0, a1, 512
.option pop
.globl insn_80
insn_80:
.option push
.option norvc
    sltiu a0, a1, 1024
.option pop
.globl insn_81
insn_81:
.option push
.option norvc
    sltiu a0, a1, 2047
.option pop
.globl insn_82
insn_82:
.option push
.option norvc
    sltiu a0, a1, -1
.option pop
.globl insn_83
insn_83:
.option push
.option norvc
    sltiu a0, a1, -2
.option pop
.globl insn_84
insn_84:
.option push
.option norvc
    sltiu a0, a1, -1024
.option pop
.globl insn_85
insn_85:
.option push
.option norvc
    sltiu a0, a1, -2048
.option pop
.globl insn_86
insn_86:
.option push
.option norvc
    sltiu a0, a1, 1365
.option pop
.globl insn_87
insn_87:
.option push
.option norvc
    sltiu a0, a1, -1366
.option pop
.globl insn_88
insn_88:
.option push
.option norvc
    xori a0, a1, 0
.option pop
.globl insn_89
insn_89:
.option push
.option norvc
    xori a0, a1, 1
.option pop
.globl insn_90
insn_90:
.option push
.option norvc
    xori a0, a1, 2
.option pop
.globl insn_91
insn_91:
.option push
.option norvc
    xori a0, a1, 4
.option pop
.globl insn_92
insn_92:
.option push
.option norvc
    xori a0, a1, 8
.option pop
.globl insn_93
insn_93:
.option push
.option norvc
    xori a0, a1, 16
.option pop
.globl insn_94
insn_94:
.option push
.option norvc
    xori a0, a1, 32
.option pop
.globl insn_95
insn_95:
.option push
.option norvc
    xori a0, a1, 64
.option pop
.globl insn_96
insn_96:
.option push
.option norvc
    xori a0, a1, 128
.option pop
.globl insn_97
insn_97:
.option push
.option norvc
    xori a0, a1, 256
.option pop
.globl insn_98
insn_98:
.option push
.option norvc
    xori a0, a1, 512
.option pop
.globl insn_99
insn_99:
.option push
.option norvc
    xori a0, a1, 1024
.option pop
.globl insn_100
insn_100:
.option push
.option norvc
    xori a0, a1, 2047
.option pop
.globl insn_101
insn_101:
.option push
.option norvc
    xori a0, a1, -1
.option pop
.globl insn_102
insn_102:
.option push
.option norvc
    xori a0, a1, -2
.option pop
.globl insn_103
insn_103:
.option push
.option norvc
    xori a0, a1, -1024
.option pop
.globl insn_104
insn_104:
.option push
.option norvc
    xori a0, a1, -2048
.option pop
.globl insn_105
insn_105:
.option push
.option norvc
    xori a0, a1, 1365
.option pop
.globl insn_106
insn_106:
.option push
.option norvc
    xori a0, a1, -1366
.option pop
.globl insn_107
insn_107:
.option push
.option norvc
    ori a0, a1, 0
.option pop
.globl insn_108
insn_108:
.option push
.option norvc
    ori a0, a1, 1
.option pop
.globl insn_109
insn_109:
.option push
.option norvc
    ori a0, a1, 2
.option pop
.globl insn_110
insn_110:
.option push
.option norvc
    ori a0, a1, 4
.option pop
.globl insn_111
insn_111:
.option push
.option norvc
    ori a0, a1, 8
.option pop
.globl insn_112
insn_112:
.option push
.option norvc
    ori a0, a1, 16
.option pop
.globl insn_113
insn_113:
.option push
.option norvc
    ori a0, a1, 32
.option pop
.globl insn_114
insn_114:
.option push
.option norvc
    ori a0, a1, 64
.option pop
.globl insn_115
insn_115:
.option push
.option norvc
    ori a0, a1, 128
.option pop
.globl insn_116
insn_116:
.option push
.option norvc
    ori a0, a1, 256
.option pop
.globl insn_117
insn_117:
.option push
.option norvc
    ori a0, a1, 512
.option pop
.globl insn_118
insn_118:
.option push
.option norvc
    ori a0, a1, 1024
.option pop
.globl insn_119
insn_119:
.option push
.option norvc
    ori a0, a1, 2047
.option pop
.globl insn_120
insn_120:
.option push
.option norvc
    ori a0, a1, -1
.option pop
.globl insn_121
insn_121:
.option push
.option norvc
    ori a0, a1, -2
.option pop
.globl insn_122
insn_122:
.option push
.option norvc
    ori a0, a1, -1024
.option pop
.globl insn_123
insn_123:
.option push
.option norvc
    ori a0, a1, -2048
.option pop
.globl insn_124
insn_124:
.option push
.option norvc
    ori a0, a1, 1365
.option pop
.globl insn_125
insn_125:
.option push
.option norvc
    ori a0, a1, -1366
.option pop
.globl insn_126
insn_126:
.option push
.option norvc
    andi a0, a1, 0
.option pop
.globl insn_127
insn_127:
.option push
.option norvc
    andi a0, a1, 1
.option pop
.globl insn_128
insn_128:
.option push
.option norvc
    andi a0, a1, 2
.option pop
.globl insn_129
insn_129:
.option push
.option norvc
    andi a0, a1, 4
.option pop
.globl insn_130
insn_130:
.option push
.option norvc
    andi a0, a1, 8
.option pop
.globl insn_131
insn_131:
.option push
.option norvc
    andi a0, a1, 16
.option pop
.globl insn_132
insn_132:
.option push
.option norvc
    andi a0, a1, 32
.option pop
.globl insn_133
insn_133:
.option push
.option norvc
    andi a0, a1, 64
.option pop
.globl insn_134
insn_134:
.option push
.option norvc
    andi a0, a1, 128
.option pop
.globl insn_135
insn_135:
.option push
.option norvc
    andi a0, a1, 256
.option pop
.globl insn_136
insn_136:
.option push
.option norvc
    andi a0, a1, 512
.option pop
.globl insn_137
insn_137:
.option push
.option norvc
    andi a0, a1, 1024
.option pop
.globl insn_138
insn_138:
.option push
.option norvc
    andi a0, a1, 2047
.option pop
.globl insn_139
insn_139:
.option push
.option norvc
    andi a0, a1, -1
.option pop
.globl insn_140
insn_140:
.option push
.option norvc
    andi a0, a1, -2
.option pop
.globl insn_141
insn_141:
.option push
.option norvc
    andi a0, a1, -1024
.option pop
.globl insn_142
insn_142:
.option push
.option norvc
    andi a0, a1, -2048
.option pop
.globl insn_143
insn_143:
.option push
.option norvc
    andi a0, a1, 1365
.option pop
.globl insn_144
insn_144:
.option push
.option norvc
    andi a0, a1, -1366
.option pop
.globl insn_145
insn_145:
.option push
.option norvc
    slli a0, a1, 0
.option pop
.globl insn_146
insn_146:
.option push
.option norvc
    srli a0, a1, 0
.option pop
.globl insn_147
insn_147:
.option push
.option norvc
    srai a0, a1, 0
.option pop
.globl insn_148
insn_148:
.option push
.option norvc
    slli a0, a1, 1
.option pop
.globl insn_149
insn_149:
.option push
.option norvc
    srli a0, a1, 1
.option pop
.globl insn_150
insn_150:
.option push
.option norvc
    srai a0, a1, 1
.option pop
.globl insn_151
insn_151:
.option push
.option norvc
    slli a0, a1, 2
.option pop
.globl insn_152
insn_152:
.option push
.option norvc
    srli a0, a1, 2
.option pop
.globl insn_153
insn_153:
.option push
.option norvc
    srai a0, a1, 2
.option pop
.globl insn_154
insn_154:
.option push
.option norvc
    slli a0, a1, 4
.option pop
.globl insn_155
insn_155:
.option push
.option norvc
    srli a0, a1, 4
.option pop
.globl insn_156
insn_156:
.option push
.option norvc
    srai a0, a1, 4
.option pop
.globl insn_157
insn_157:
.option push
.option norvc
    slli a0, a1, 8
.option pop
.globl insn_158
insn_158:
.option push
.option norvc
    srli a0, a1, 8
.option pop
.globl insn_159
insn_159:
.option push
.option norvc
    srai a0, a1, 8
.option pop
.globl insn_160
insn_160:
.option push
.option norvc
    slli a0, a1, 16
.option pop
.globl insn_161
insn_161:
.option push
.option norvc
    srli a0, a1, 16
.option pop
.globl insn_162
insn_162:
.option push
.option norvc
    srai a0, a1, 16
.option pop
.globl insn_163
insn_163:
.option push
.option norvc
    slli a0, a1, 21
.option pop
.globl insn_164
insn_164:
.option push
.option norvc
    srli a0, a1, 21
.option pop
.globl insn_165
insn_165:
.option push
.option norvc
    srai a0, a1, 21
.option pop
.globl insn_166
insn_166:
.option push
.option norvc
    slli a0, a1, 31
.option pop
.globl insn_167
insn_167:
.option push
.option norvc
    srli a0, a1, 31
.option pop
.globl insn_168
insn_168:
.option push
.option norvc
    srai a0, a1, 31
.option pop
.globl insn_169
insn_169:
.option push
.option norvc
    lui a0, 0x0
.option pop
.globl insn_170
insn_170:
.option push
.option norvc
    auipc a0, 0x0
.option pop
.globl insn_171
insn_171:
.option push
.option norvc
    lui a0, 0xfffff
.option pop
.globl insn_172
insn_172:
.option push
.option norvc
    auipc a0, 0xfffff
.option pop
.globl insn_173
insn_173:
.option push
.option norvc
    lui a0, 0x12345
.option pop
.globl insn_174
insn_174:
.option push
.option norvc
    auipc a0, 0x12345
.option pop
.globl insn_175
insn_175:
.option push
.option norvc
    lui a0, 0xaaaaa
.option pop
.globl insn_176
insn_176:
.option push
.option norvc
    auipc a0, 0xaaaaa
.option pop
.globl insn_177
insn_177:
.option push
.option norvc
    lui a0, 0x55555
.option pop
.globl insn_178
insn_178:
.option push
.option norvc
    auipc a0, 0x55555
.option pop
.globl insn_179
insn_179:
.option push
.option norvc
    lui a0, 0x1
.option pop
.globl insn_180
insn_180:
.option push
.option norvc
    auipc a0, 0x1
.option pop
.globl insn_181
insn_181:
.option push
.option norvc
    lui a0, 0x2
.option pop
.globl insn_182
insn_182:
.option push
.option norvc
    auipc a0, 0x2
.option pop
.globl insn_183
insn_183:
.option push
.option norvc
    lui a0, 0x4
.option pop
.globl insn_184
insn_184:
.option push
.option norvc
    auipc a0, 0x4
.option pop
.globl insn_185
insn_185:
.option push
.option norvc
    lui a0, 0x8
.option pop
.globl insn_186
insn_186:
.option push
.option norvc
    auipc a0, 0x8
.option pop
.globl insn_187
insn_187:
.option push
.option norvc
    lui a0, 0x10
.option pop
.globl insn_188
insn_188:
.option push
.option norvc
    auipc a0, 0x10
.option pop
.globl insn_189
insn_189:
.option push
.option norvc
    lui a0, 0x20
.option pop
.globl insn_190
insn_190:
.option push
.option norvc
    auipc a0, 0x20
.option pop
.globl insn_191
insn_191:
.option push
.option norvc
    lui a0, 0x40
.option pop
.globl insn_192
insn_192:
.option push
.option norvc
    auipc a0, 0x40
.option pop
.globl insn_193
insn_193:
.option push
.option norvc
    lui a0, 0x80
.option pop
.globl insn_194
insn_194:
.option push
.option norvc
    auipc a0, 0x80
.option pop
.globl insn_195
insn_195:
.option push
.option norvc
    lui a0, 0x100
.option pop
.globl insn_196
insn_196:
.option push
.option norvc
    auipc a0, 0x100
.option pop
.globl insn_197
insn_197:
.option push
.option norvc
    lui a0, 0x200
.option pop
.globl insn_198
insn_198:
.option push
.option norvc
    auipc a0, 0x200
.option pop
.globl insn_199
insn_199:
.option push
.option norvc
    lui a0, 0x400
.option pop
.globl insn_200
insn_200:
.option push
.option norvc
    auipc a0, 0x400
.option pop
.globl insn_201
insn_201:
.option push
.option norvc
    lui a0, 0x800
.option pop
.globl insn_202
insn_202:
.option push
.option norvc
    auipc a0, 0x800
.option pop
.globl insn_203
insn_203:
.option push
.option norvc
    lui a0, 0x1000
.option pop
.globl insn_204
insn_204:
.option push
.option norvc
    auipc a0, 0x1000
.option pop
.globl insn_205
insn_205:
.option push
.option norvc
    lui a0, 0x2000
.option pop
.globl insn_206
insn_206:
.option push
.option norvc
    auipc a0, 0x2000
.option pop
.globl insn_207
insn_207:
.option push
.option norvc
    lui a0, 0x4000
.option pop
.globl insn_208
insn_208:
.option push
.option norvc
    auipc a0, 0x4000
.option pop
.globl insn_209
insn_209:
.option push
.option norvc
    lui a0, 0x8000
.option pop
.globl insn_210
insn_210:
.option push
.option norvc
    auipc a0, 0x8000
.option pop
.globl insn_211
insn_211:
.option push
.option norvc
    lui a0, 0x10000
.option pop
.globl insn_212
insn_212:
.option push
.option norvc
    auipc a0, 0x10000
.option pop
.globl insn_213
insn_213:
.option push
.option norvc
    lui a0, 0x20000
.option pop
.globl insn_214
insn_214:
.option push
.option norvc
    auipc a0, 0x20000
.option pop
.globl insn_215
insn_215:
.option push
.option norvc
    lui a0, 0x40000
.option pop
.globl insn_216
insn_216:
.option push
.option norvc
    auipc a0, 0x40000
.option pop
.globl insn_217
insn_217:
.option push
.option norvc
    lui a0, 0x80000
.option pop
.globl insn_218
insn_218:
.option push
.option norvc
    auipc a0, 0x80000
.option pop
.globl insn_219
insn_219:
.option push
.option norvc
    jal ra, 0
.option pop
.globl insn_220
insn_220:
.option push
.option norvc
    jal ra, -2
.option pop
.globl insn_221
insn_221:
.option push
.option norvc
    jal ra, 1048574
.option pop
.globl insn_222
insn_222:
.option push
.option norvc
    jal ra, -1048576
.option pop
.globl insn_223
insn_223:
.option push
.option norvc
    jal ra, 699050
.option pop
.globl insn_224
insn_224:
.option push
.option norvc
    jal ra, -699052
.option pop
.globl insn_225
insn_225:
.option push
.option norvc
    jal ra, 349524
.option pop
.globl insn_226
insn_226:
.option push
.option norvc
    jal ra, 2
.option pop
.globl insn_227
insn_227:
.option push
.option norvc
    jal ra, 4
.option pop
.globl insn_228
insn_228:
.option push
.option norvc
    jal ra, 8
.option pop
.globl insn_229
insn_229:
.option push
.option norvc
    jal ra, 16
.option pop
.globl insn_230
insn_230:
.option push
.option norvc
    jal ra, 32
.option pop
.globl insn_231
insn_231:
.option push
.option norvc
    jal ra, 64
.option pop
.globl insn_232
insn_232:
.option push
.option norvc
    jal ra, 128
.option pop
.globl insn_233
insn_233:
.option push
.option norvc
    jal ra, 256
.option pop
.globl insn_234
insn_234:
.option push
.option norvc
    jal ra, 512
.option pop
.globl insn_235
insn_235:
.option push
.option norvc
    jal ra, 1024
.option pop
.globl insn_236
insn_236:
.option push
.option norvc
    jal ra, 2048
.option pop
.globl insn_237
insn_237:
.option push
.option norvc
    jal ra, 4096
.option pop
.globl insn_238
insn_238:
.option push
.option norvc
    jal ra, 8192
.option pop
.globl insn_239
insn_239:
.option push
.option norvc
    jal ra, 16384
.option pop
.globl insn_240
insn_240:
.option push
.option norvc
    jal ra, 32768
.option pop
.globl insn_241
insn_241:
.option push
.option norvc
    jal ra, 65536
.option pop
.globl insn_242
insn_242:
.option push
.option norvc
    jal ra, 131072
.option pop
.globl insn_243
insn_243:
.option push
.option norvc
    jal ra, 262144
.option pop
.globl insn_244
insn_244:
.option push
.option norvc
    jal ra, 524288
.option pop
.globl insn_245
insn_245:
.option push
.option norvc
    jal zero, 8
.option pop
.globl insn_246
insn_246:
.option push
.option norvc
    jal t6, -8
.option pop
.globl insn_247
insn_247:
.option push
.option norvc
    jalr ra, 0(a0)
.option pop
.globl insn_248
insn_248:
.option push
.option norvc
    jalr ra, 5(a0)
.option pop
.globl insn_249
insn_249:
.option push
.option norvc
    jalr a0, 0(a0)
.option pop
.globl insn_250
insn_250:
.option push
.option norvc
    jalr zero, -4(a0)
.option pop
.globl insn_251
insn_251:
.option push
.option norvc
    jalr ra, -2048(a0)
.option pop
.globl insn_252
insn_252:
.option push
.option norvc
    jalr ra, 2047(a0)
.option pop
.globl insn_253
insn_253:
.option push
.option norvc
    jalr t6, 1(t5)
.option pop
.globl insn_254
insn_254:
.option push
.option norvc
    beq a0, a1, 16
.option pop
.globl insn_255
insn_255:
.option push
.option norvc
    bne a0, a1, 16
.option pop
.globl insn_256
insn_256:
.option push
.option norvc
    blt a0, a1, 16
.option pop
.globl insn_257
insn_257:
.option push
.option norvc
    bge a0, a1, 16
.option pop
.globl insn_258
insn_258:
.option push
.option norvc
    bltu a0, a1, 16
.option pop
.globl insn_259
insn_259:
.option push
.option norvc
    bgeu a0, a1, 16
.option pop
.globl insn_260
insn_260:
.option push
.option norvc
    beq a0, a0, -2
.option pop
.globl insn_261
insn_261:
.option push
.option norvc
    bne a0, a0, -2
.option pop
.globl insn_262
insn_262:
.option push
.option norvc
    bltu t5, t6, -2
.option pop
.globl insn_263
insn_263:
.option push
.option norvc
    beq a0, a0, 4094
.option pop
.globl insn_264
insn_264:
.option push
.option norvc
    bne a0, a0, 4094
.option pop
.globl insn_265
insn_265:
.option push
.option norvc
    bltu t5, t6, 4094
.option pop
.globl insn_266
insn_266:
.option push
.option norvc
    beq a0, a0, -4096
.option pop
.globl insn_267
insn_267:
.option push
.option norvc
    bne a0, a0, -4096
.option pop
.globl insn_268
insn_268:
.option push
.option norvc
    bltu t5, t6, -4096
.option pop
.globl insn_269
insn_269:
.option push
.option norvc
    beq a0, a0, 2730
.option pop
.globl insn_270
insn_270:
.option push
.option norvc
    bne a0, a0, 2730
.option pop
.globl insn_271
insn_271:
.option push
.option norvc
    bltu t5, t6, 2730
.option pop
.globl insn_272
insn_272:
.option push
.option norvc
    beq a0, a0, -2732
.option pop
.globl insn_273
insn_273:
.option push
.option norvc
    bne a0, a0, -2732
.option pop
.globl insn_274
insn_274:
.option push
.option norvc
    bltu t5, t6, -2732
.option pop
.globl insn_275
insn_275:
.option push
.option norvc
    beq a0, a0, 1364
.option pop
.globl insn_276
insn_276:
.option push
.option norvc
    bne a0, a0, 1364
.option pop
.globl insn_277
insn_277:
.option push
.option norvc
    bltu t5, t6, 1364
.option pop
.globl insn_278
insn_278:
.option push
.option norvc
    beq a0, a0, 2
.option pop
.globl insn_279
insn_279:
.option push
.option norvc
    bne a0, a0, 2
.option pop
.globl insn_280
insn_280:
.option push
.option norvc
    bltu t5, t6, 2
.option pop
.globl insn_281
insn_281:
.option push
.option norvc
    beq a0, a0, 4
.option pop
.globl insn_282
insn_282:
.option push
.option norvc
    bne a0, a0, 4
.option pop
.globl insn_283
insn_283:
.option push
.option norvc
    bltu t5, t6, 4
.option pop
.globl insn_284
insn_284:
.option push
.option norvc
    beq a0, a0, 8
.option pop
.globl insn_285
insn_285:
.option push
.option norvc
    bne a0, a0, 8
.option pop
.globl insn_286
insn_286:
.option push
.option norvc
    bltu t5, t6, 8
.option pop
.globl insn_287
insn_287:
.option push
.option norvc
    beq a0, a0, 16
.option pop
.globl insn_288
insn_288:
.option push
.option norvc
    bne a0, a0, 16
.option pop
.globl insn_289
insn_289:
.option push
.option norvc
    bltu t5, t6, 16
.option pop
.globl insn_290
insn_290:
.option push
.option norvc
    beq a0, a0, 32
.option pop
.globl insn_291
insn_291:
.option push
.option norvc
    bne a0, a0, 32
.option pop
.globl insn_292
insn_292:
.option push
.option norvc
    bltu t5, t6, 32
.option pop
.globl insn_293
insn_293:
.option push
.option norvc
    beq a0, a0, 64
.option pop
.globl insn_294
insn_294:
.option push
.option norvc
    bne a0, a0, 64
.option pop
.globl insn_295
insn_295:
.option push
.option norvc
    bltu t5, t6, 64
.option pop
.globl insn_296
insn_296:
.option push
.option norvc
    beq a0, a0, 128
.option pop
.globl insn_297
insn_297:
.option push
.option norvc
    bne a0, a0, 128
.option pop
.globl insn_298
insn_298:
.option push
.option norvc
    bltu t5, t6, 128
.option pop
.globl insn_299
insn_299:
.option push
.option norvc
    beq a0, a0, 256
.option pop
.globl insn_300
insn_300:
.option push
.option norvc
    bne a0, a0, 256
.option pop
.globl insn_301
insn_301:
.option push
.option norvc
    bltu t5, t6, 256
.option pop
.globl insn_302
insn_302:
.option push
.option norvc
    beq a0, a0, 512
.option pop
.globl insn_303
insn_303:
.option push
.option norvc
    bne a0, a0, 512
.option pop
.globl insn_304
insn_304:
.option push
.option norvc
    bltu t5, t6, 512
.option pop
.globl insn_305
insn_305:
.option push
.option norvc
    beq a0, a0, 1024
.option pop
.globl insn_306
insn_306:
.option push
.option norvc
    bne a0, a0, 1024
.option pop
.globl insn_307
insn_307:
.option push
.option norvc
    bltu t5, t6, 1024
.option pop
.globl insn_308
insn_308:
.option push
.option norvc
    beq a0, a0, 2048
.option pop
.globl insn_309
insn_309:
.option push
.option norvc
    bne a0, a0, 2048
.option pop
.globl insn_310
insn_310:
.option push
.option norvc
    bltu t5, t6, 2048
.option pop
.globl insn_311
insn_311:
.option push
.option norvc
    lb a0, 0(a1)
.option pop
.globl insn_312
insn_312:
.option push
.option norvc
    lbu a0, 0(a1)
.option pop
.globl insn_313
insn_313:
.option push
.option norvc
    lh a0, 0(a1)
.option pop
.globl insn_314
insn_314:
.option push
.option norvc
    lhu a0, 0(a1)
.option pop
.globl insn_315
insn_315:
.option push
.option norvc
    lw a0, 0(a1)
.option pop
.globl insn_316
insn_316:
.option push
.option norvc
    lb a0, 1(a1)
.option pop
.globl insn_317
insn_317:
.option push
.option norvc
    lbu a0, 1(a1)
.option pop
.globl insn_318
insn_318:
.option push
.option norvc
    lh a0, 1(a1)
.option pop
.globl insn_319
insn_319:
.option push
.option norvc
    lhu a0, 1(a1)
.option pop
.globl insn_320
insn_320:
.option push
.option norvc
    lw a0, 1(a1)
.option pop
.globl insn_321
insn_321:
.option push
.option norvc
    lb a0, 2(a1)
.option pop
.globl insn_322
insn_322:
.option push
.option norvc
    lbu a0, 2(a1)
.option pop
.globl insn_323
insn_323:
.option push
.option norvc
    lh a0, 2(a1)
.option pop
.globl insn_324
insn_324:
.option push
.option norvc
    lhu a0, 2(a1)
.option pop
.globl insn_325
insn_325:
.option push
.option norvc
    lw a0, 2(a1)
.option pop
.globl insn_326
insn_326:
.option push
.option norvc
    lb a0, 3(a1)
.option pop
.globl insn_327
insn_327:
.option push
.option norvc
    lbu a0, 3(a1)
.option pop
.globl insn_328
insn_328:
.option push
.option norvc
    lh a0, 3(a1)
.option pop
.globl insn_329
insn_329:
.option push
.option norvc
    lhu a0, 3(a1)
.option pop
.globl insn_330
insn_330:
.option push
.option norvc
    lw a0, 3(a1)
.option pop
.globl insn_331
insn_331:
.option push
.option norvc
    lb a0, 4(a1)
.option pop
.globl insn_332
insn_332:
.option push
.option norvc
    lbu a0, 4(a1)
.option pop
.globl insn_333
insn_333:
.option push
.option norvc
    lh a0, 4(a1)
.option pop
.globl insn_334
insn_334:
.option push
.option norvc
    lhu a0, 4(a1)
.option pop
.globl insn_335
insn_335:
.option push
.option norvc
    lw a0, 4(a1)
.option pop
.globl insn_336
insn_336:
.option push
.option norvc
    lb a0, 8(a1)
.option pop
.globl insn_337
insn_337:
.option push
.option norvc
    lbu a0, 8(a1)
.option pop
.globl insn_338
insn_338:
.option push
.option norvc
    lh a0, 8(a1)
.option pop
.globl insn_339
insn_339:
.option push
.option norvc
    lhu a0, 8(a1)
.option pop
.globl insn_340
insn_340:
.option push
.option norvc
    lw a0, 8(a1)
.option pop
.globl insn_341
insn_341:
.option push
.option norvc
    lb a0, 16(a1)
.option pop
.globl insn_342
insn_342:
.option push
.option norvc
    lbu a0, 16(a1)
.option pop
.globl insn_343
insn_343:
.option push
.option norvc
    lh a0, 16(a1)
.option pop
.globl insn_344
insn_344:
.option push
.option norvc
    lhu a0, 16(a1)
.option pop
.globl insn_345
insn_345:
.option push
.option norvc
    lw a0, 16(a1)
.option pop
.globl insn_346
insn_346:
.option push
.option norvc
    lb a0, 32(a1)
.option pop
.globl insn_347
insn_347:
.option push
.option norvc
    lbu a0, 32(a1)
.option pop
.globl insn_348
insn_348:
.option push
.option norvc
    lh a0, 32(a1)
.option pop
.globl insn_349
insn_349:
.option push
.option norvc
    lhu a0, 32(a1)
.option pop
.globl insn_350
insn_350:
.option push
.option norvc
    lw a0, 32(a1)
.option pop
.globl insn_351
insn_351:
.option push
.option norvc
    lb a0, 64(a1)
.option pop
.globl insn_352
insn_352:
.option push
.option norvc
    lbu a0, 64(a1)
.option pop
.globl insn_353
insn_353:
.option push
.option norvc
    lh a0, 64(a1)
.option pop
.globl insn_354
insn_354:
.option push
.option norvc
    lhu a0, 64(a1)
.option pop
.globl insn_355
insn_355:
.option push
.option norvc
    lw a0, 64(a1)
.option pop
.globl insn_356
insn_356:
.option push
.option norvc
    lb a0, 128(a1)
.option pop
.globl insn_357
insn_357:
.option push
.option norvc
    lbu a0, 128(a1)
.option pop
.globl insn_358
insn_358:
.option push
.option norvc
    lh a0, 128(a1)
.option pop
.globl insn_359
insn_359:
.option push
.option norvc
    lhu a0, 128(a1)
.option pop
.globl insn_360
insn_360:
.option push
.option norvc
    lw a0, 128(a1)
.option pop
.globl insn_361
insn_361:
.option push
.option norvc
    lb a0, 256(a1)
.option pop
.globl insn_362
insn_362:
.option push
.option norvc
    lbu a0, 256(a1)
.option pop
.globl insn_363
insn_363:
.option push
.option norvc
    lh a0, 256(a1)
.option pop
.globl insn_364
insn_364:
.option push
.option norvc
    lhu a0, 256(a1)
.option pop
.globl insn_365
insn_365:
.option push
.option norvc
    lw a0, 256(a1)
.option pop
.globl insn_366
insn_366:
.option push
.option norvc
    lb a0, 512(a1)
.option pop
.globl insn_367
insn_367:
.option push
.option norvc
    lbu a0, 512(a1)
.option pop
.globl insn_368
insn_368:
.option push
.option norvc
    lh a0, 512(a1)
.option pop
.globl insn_369
insn_369:
.option push
.option norvc
    lhu a0, 512(a1)
.option pop
.globl insn_370
insn_370:
.option push
.option norvc
    lw a0, 512(a1)
.option pop
.globl insn_371
insn_371:
.option push
.option norvc
    lb a0, 1024(a1)
.option pop
.globl insn_372
insn_372:
.option push
.option norvc
    lbu a0, 1024(a1)
.option pop
.globl insn_373
insn_373:
.option push
.option norvc
    lh a0, 1024(a1)
.option pop
.globl insn_374
insn_374:
.option push
.option norvc
    lhu a0, 1024(a1)
.option pop
.globl insn_375
insn_375:
.option push
.option norvc
    lw a0, 1024(a1)
.option pop
.globl insn_376
insn_376:
.option push
.option norvc
    lb a0, 2047(a1)
.option pop
.globl insn_377
insn_377:
.option push
.option norvc
    lbu a0, 2047(a1)
.option pop
.globl insn_378
insn_378:
.option push
.option norvc
    lh a0, 2047(a1)
.option pop
.globl insn_379
insn_379:
.option push
.option norvc
    lhu a0, 2047(a1)
.option pop
.globl insn_380
insn_380:
.option push
.option norvc
    lw a0, 2047(a1)
.option pop
.globl insn_381
insn_381:
.option push
.option norvc
    lb a0, -1(a1)
.option pop
.globl insn_382
insn_382:
.option push
.option norvc
    lbu a0, -1(a1)
.option pop
.globl insn_383
insn_383:
.option push
.option norvc
    lh a0, -1(a1)
.option pop
.globl insn_384
insn_384:
.option push
.option norvc
    lhu a0, -1(a1)
.option pop
.globl insn_385
insn_385:
.option push
.option norvc
    lw a0, -1(a1)
.option pop
.globl insn_386
insn_386:
.option push
.option norvc
    lb a0, -4(a1)
.option pop
.globl insn_387
insn_387:
.option push
.option norvc
    lbu a0, -4(a1)
.option pop
.globl insn_388
insn_388:
.option push
.option norvc
    lh a0, -4(a1)
.option pop
.globl insn_389
insn_389:
.option push
.option norvc
    lhu a0, -4(a1)
.option pop
.globl insn_390
insn_390:
.option push
.option norvc
    lw a0, -4(a1)
.option pop
.globl insn_391
insn_391:
.option push
.option norvc
    lb a0, -2048(a1)
.option pop
.globl insn_392
insn_392:
.option push
.option norvc
    lbu a0, -2048(a1)
.option pop
.globl insn_393
insn_393:
.option push
.option norvc
    lh a0, -2048(a1)
.option pop
.globl insn_394
insn_394:
.option push
.option norvc
    lhu a0, -2048(a1)
.option pop
.globl insn_395
insn_395:
.option push
.option norvc
    lw a0, -2048(a1)
.option pop
.globl insn_396
insn_396:
.option push
.option norvc
    lb a0, 1365(a1)
.option pop
.globl insn_397
insn_397:
.option push
.option norvc
    lbu a0, 1365(a1)
.option pop
.globl insn_398
insn_398:
.option push
.option norvc
    lh a0, 1365(a1)
.option pop
.globl insn_399
insn_399:
.option push
.option norvc
    lhu a0, 1365(a1)
.option pop
.globl insn_400
insn_400:
.option push
.option norvc
    lw a0, 1365(a1)
.option pop
.globl insn_401
insn_401:
.option push
.option norvc
    lb a0, -1366(a1)
.option pop
.globl insn_402
insn_402:
.option push
.option norvc
    lbu a0, -1366(a1)
.option pop
.globl insn_403
insn_403:
.option push
.option norvc
    lh a0, -1366(a1)
.option pop
.globl insn_404
insn_404:
.option push
.option norvc
    lhu a0, -1366(a1)
.option pop
.globl insn_405
insn_405:
.option push
.option norvc
    lw a0, -1366(a1)
.option pop
.globl insn_406
insn_406:
.option push
.option norvc
    lw zero, 0(a1)
.option pop
.globl insn_407
insn_407:
.option push
.option norvc
    lw a1, 0(a1)
.option pop
.globl insn_408
insn_408:
.option push
.option norvc
    sb a0, 0(a1)
.option pop
.globl insn_409
insn_409:
.option push
.option norvc
    sh a0, 0(a1)
.option pop
.globl insn_410
insn_410:
.option push
.option norvc
    sw a0, 0(a1)
.option pop
.globl insn_411
insn_411:
.option push
.option norvc
    sb a0, 1(a1)
.option pop
.globl insn_412
insn_412:
.option push
.option norvc
    sh a0, 1(a1)
.option pop
.globl insn_413
insn_413:
.option push
.option norvc
    sw a0, 1(a1)
.option pop
.globl insn_414
insn_414:
.option push
.option norvc
    sb a0, 2(a1)
.option pop
.globl insn_415
insn_415:
.option push
.option norvc
    sh a0, 2(a1)
.option pop
.globl insn_416
insn_416:
.option push
.option norvc
    sw a0, 2(a1)
.option pop
.globl insn_417
insn_417:
.option push
.option norvc
    sb a0, 3(a1)
.option pop
.globl insn_418
insn_418:
.option push
.option norvc
    sh a0, 3(a1)
.option pop
.globl insn_419
insn_419:
.option push
.option norvc
    sw a0, 3(a1)
.option pop
.globl insn_420
insn_420:
.option push
.option norvc
    sb a0, 4(a1)
.option pop
.globl insn_421
insn_421:
.option push
.option norvc
    sh a0, 4(a1)
.option pop
.globl insn_422
insn_422:
.option push
.option norvc
    sw a0, 4(a1)
.option pop
.globl insn_423
insn_423:
.option push
.option norvc
    sb a0, 8(a1)
.option pop
.globl insn_424
insn_424:
.option push
.option norvc
    sh a0, 8(a1)
.option pop
.globl insn_425
insn_425:
.option push
.option norvc
    sw a0, 8(a1)
.option pop
.globl insn_426
insn_426:
.option push
.option norvc
    sb a0, 16(a1)
.option pop
.globl insn_427
insn_427:
.option push
.option norvc
    sh a0, 16(a1)
.option pop
.globl insn_428
insn_428:
.option push
.option norvc
    sw a0, 16(a1)
.option pop
.globl insn_429
insn_429:
.option push
.option norvc
    sb a0, 32(a1)
.option pop
.globl insn_430
insn_430:
.option push
.option norvc
    sh a0, 32(a1)
.option pop
.globl insn_431
insn_431:
.option push
.option norvc
    sw a0, 32(a1)
.option pop
.globl insn_432
insn_432:
.option push
.option norvc
    sb a0, 64(a1)
.option pop
.globl insn_433
insn_433:
.option push
.option norvc
    sh a0, 64(a1)
.option pop
.globl insn_434
insn_434:
.option push
.option norvc
    sw a0, 64(a1)
.option pop
.globl insn_435
insn_435:
.option push
.option norvc
    sb a0, 128(a1)
.option pop
.globl insn_436
insn_436:
.option push
.option norvc
    sh a0, 128(a1)
.option pop
.globl insn_437
insn_437:
.option push
.option norvc
    sw a0, 128(a1)
.option pop
.globl insn_438
insn_438:
.option push
.option norvc
    sb a0, 256(a1)
.option pop
.globl insn_439
insn_439:
.option push
.option norvc
    sh a0, 256(a1)
.option pop
.globl insn_440
insn_440:
.option push
.option norvc
    sw a0, 256(a1)
.option pop
.globl insn_441
insn_441:
.option push
.option norvc
    sb a0, 512(a1)
.option pop
.globl insn_442
insn_442:
.option push
.option norvc
    sh a0, 512(a1)
.option pop
.globl insn_443
insn_443:
.option push
.option norvc
    sw a0, 512(a1)
.option pop
.globl insn_444
insn_444:
.option push
.option norvc
    sb a0, 1024(a1)
.option pop
.globl insn_445
insn_445:
.option push
.option norvc
    sh a0, 1024(a1)
.option pop
.globl insn_446
insn_446:
.option push
.option norvc
    sw a0, 1024(a1)
.option pop
.globl insn_447
insn_447:
.option push
.option norvc
    sb a0, 2047(a1)
.option pop
.globl insn_448
insn_448:
.option push
.option norvc
    sh a0, 2047(a1)
.option pop
.globl insn_449
insn_449:
.option push
.option norvc
    sw a0, 2047(a1)
.option pop
.globl insn_450
insn_450:
.option push
.option norvc
    sb a0, -1(a1)
.option pop
.globl insn_451
insn_451:
.option push
.option norvc
    sh a0, -1(a1)
.option pop
.globl insn_452
insn_452:
.option push
.option norvc
    sw a0, -1(a1)
.option pop
.globl insn_453
insn_453:
.option push
.option norvc
    sb a0, -4(a1)
.option pop
.globl insn_454
insn_454:
.option push
.option norvc
    sh a0, -4(a1)
.option pop
.globl insn_455
insn_455:
.option push
.option norvc
    sw a0, -4(a1)
.option pop
.globl insn_456
insn_456:
.option push
.option norvc
    sb a0, -2048(a1)
.option pop
.globl insn_457
insn_457:
.option push
.option norvc
    sh a0, -2048(a1)
.option pop
.globl insn_458
insn_458:
.option push
.option norvc
    sw a0, -2048(a1)
.option pop
.globl insn_459
insn_459:
.option push
.option norvc
    sb a0, 1365(a1)
.option pop
.globl insn_460
insn_460:
.option push
.option norvc
    sh a0, 1365(a1)
.option pop
.globl insn_461
insn_461:
.option push
.option norvc
    sw a0, 1365(a1)
.option pop
.globl insn_462
insn_462:
.option push
.option norvc
    sb a0, -1366(a1)
.option pop
.globl insn_463
insn_463:
.option push
.option norvc
    sh a0, -1366(a1)
.option pop
.globl insn_464
insn_464:
.option push
.option norvc
    sw a0, -1366(a1)
.option pop
.globl insn_465
insn_465:
.option push
.option norvc
    sw zero, 0(a1)
.option pop
.globl insn_466
insn_466:
.option push
.option norvc
    sw a1, 0(a1)
.option pop
.globl insn_467
insn_467:
.option push
.option norvc
    lw a0, -1(zero)
.option pop
.globl insn_468
insn_468:
.option push
.option norvc
    fence
.option pop
.globl insn_469
insn_469:
.option push
.option norvc
    fence rw, w
.option pop
.globl insn_470
insn_470:
.option push
.option norvc
    fence.tso
.option pop
.globl insn_471
insn_471:
.option push
.option norvc
    pause
.option pop
.globl insn_472
insn_472:
.option push
.option norvc
    nop
.option pop
.globl insn_473
insn_473:
.option push
.option norvc
    ebreak
.option pop
.globl insn_474
insn_474:
.option push
.option rvc
    c.ebreak
.option pop
.globl insn_475
insn_475:
.option push
.option norvc
    addi a0, zero, 1
    addi a0, a0, 1
    ebreak
.option pop
.globl insn_476
insn_476:
.option push
.option norvc
    lr.w a0, (a1)
.option pop
.globl insn_477
insn_477:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a2, a3, (a1)
.option pop
.globl insn_478
insn_478:
.option push
.option norvc
    sc.w a2, a3, (a1)
.option pop
.globl insn_479
insn_479:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a2, a3, (a4)
    sc.w a5, a3, (a1)
.option pop
.globl insn_480
insn_480:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a2, a3, (a1)
    sc.w a5, a0, (a1)
.option pop
.globl insn_481
insn_481:
.option push
.option norvc
    lr.w a0, (a1)
    lr.w a0, (a4)
    sc.w a2, a3, (a1)
.option pop
.globl insn_482
insn_482:
.option push
.option norvc
    lr.w a0, (a1)
    sw a4, 0(a1)
    sc.w a2, a3, (a1)
.option pop
.globl insn_483
insn_483:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a3, a3, (a1)
.option pop
.globl insn_484
insn_484:
.option push
.option norvc
    lr.w.aqrl a0, (a1)
    sc.w.aqrl a2, a3, (a1)
.option pop
.globl insn_485
insn_485:
.option push
.option norvc
    lr.w a1, (a1)
    sc.w a2, a3, (a4)
.option pop
.globl insn_486
insn_486:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a1, a3, (a1)
.option pop
.globl insn_487
insn_487:
.option push
.option norvc
    sc.w a1, a3, (a1)
.option pop
.globl insn_488
insn_488:
.option push
.option norvc
    lr.w zero, (a1)
    sc.w a2, a3, (a1)
.option pop
.globl insn_489
insn_489:
.option push
.option norvc
    amoswap.w a0, a2, (a1)
.option pop
.globl insn_490
insn_490:
.option push
.option norvc
    amoswap.w.aqrl a0, a2, (a1)
.option pop
.globl insn_491
insn_491:
.option push
.option norvc
    amoadd.w a0, a2, (a1)
.option pop
.globl insn_492
insn_492:
.option push
.option norvc
    amoadd.w.aqrl a0, a2, (a1)
.option pop
.globl insn_493
insn_493:
.option push
.option norvc
    amoxor.w a0, a2, (a1)
.option pop
.globl insn_494
insn_494:
.option push
.option norvc
    amoxor.w.aqrl a0, a2, (a1)
.option pop
.globl insn_495
insn_495:
.option push
.option norvc
    amoand.w a0, a2, (a1)
.option pop
.globl insn_496
insn_496:
.option push
.option norvc
    amoand.w.aqrl a0, a2, (a1)
.option pop
.globl insn_497
insn_497:
.option push
.option norvc
    amoor.w a0, a2, (a1)
.option pop
.globl insn_498
insn_498:
.option push
.option norvc
    amoor.w.aqrl a0, a2, (a1)
.option pop
.globl insn_499
insn_499:
.option push
.option norvc
    amomin.w a0, a2, (a1)
.option pop
.globl insn_500
insn_500:
.option push
.option norvc
    amomin.w.aqrl a0, a2, (a1)
.option pop
.globl insn_501
insn_501:
.option push
.option norvc
    amomax.w a0, a2, (a1)
.option pop
.globl insn_502
insn_502:
.option push
.option norvc
    amomax.w.aqrl a0, a2, (a1)
.option pop
.globl insn_503
insn_503:
.option push
.option norvc
    amominu.w a0, a2, (a1)
.option pop
.globl insn_504
insn_504:
.option push
.option norvc
    amominu.w.aqrl a0, a2, (a1)
.option pop
.globl insn_505
insn_505:
.option push
.option norvc
    amomaxu.w a0, a2, (a1)
.option pop
.globl insn_506
insn_506:
.option push
.option norvc
    amomaxu.w.aqrl a0, a2, (a1)
.option pop
.globl insn_507
insn_507:
.option push
.option norvc
    amoadd.w a2, a2, (a1)
.option pop
.globl insn_508
insn_508:
.option push
.option norvc
    amoswap.w a1, a2, (a1)
.option pop
.globl insn_509
insn_509:
.option push
.option norvc
    amoadd.w zero, a2, (a1)
.option pop
.globl insn_510
insn_510:
.option push
.option norvc
    lr.w a0, (a1)
    amoadd.w a4, a2, (a1)
    sc.w a5, a3, (a1)
.option pop
.globl insn_511
insn_511:
.option push
.option norvc
    sc.w a0, a2, (a1)
.option pop
.globl insn_512
insn_512:
.option push
.option norvc
    lr.w a0, (a1)
    sc.w a2, a3, (a4)
.option pop
.globl insn_513
insn_513:
.option push
.option norvc
    addi a0, zero, 0
    addi a1, zero, 10
    1:
    add a0, a0, a1
    addi a1, a1, -1
    bne a1, zero, 1b
.option pop
.globl insn_514
insn_514:
.option push
.option rvc
    c.li a0, 0
    c.li a1, 10
    1:
    c.add a0, a1
    c.addi a1, -1
    c.bnez a1, 1b
.option pop
.globl insn_515
insn_515:
.option push
.option norvc
    jal ra, 2f
    addi a0, a0, 1
    jal zero, 3f
    2:
    addi a0, a0, 16
    jalr zero, 0(ra)
    3:
.option pop
.globl insn_516
insn_516:
.option push
.option rvc
    c.jal 2f
    c.addi a0, 1
    c.j 3f
    2:
    c.addi a0, 16
    c.jr ra
    3:
.option pop
.globl insn_517
insn_517:
.option push
.option rvc
    c.addi4spn a0, sp, 4
.option pop
.globl insn_518
insn_518:
.option push
.option rvc
    c.addi4spn a0, sp, 8
.option pop
.globl insn_519
insn_519:
.option push
.option rvc
    c.addi4spn a0, sp, 16
.option pop
.globl insn_520
insn_520:
.option push
.option rvc
    c.addi4spn a0, sp, 32
.option pop
.globl insn_521
insn_521:
.option push
.option rvc
    c.addi4spn a0, sp, 64
.option pop
.globl insn_522
insn_522:
.option push
.option rvc
    c.addi4spn a0, sp, 128
.option pop
.globl insn_523
insn_523:
.option push
.option rvc
    c.addi4spn a0, sp, 256
.option pop
.globl insn_524
insn_524:
.option push
.option rvc
    c.addi4spn a0, sp, 512
.option pop
.globl insn_525
insn_525:
.option push
.option rvc
    c.addi4spn a0, sp, 1020
.option pop
.globl insn_526
insn_526:
.option push
.option rvc
    c.addi4spn a0, sp, 340
.option pop
.globl insn_527
insn_527:
.option push
.option rvc
    c.addi4spn a0, sp, 680
.option pop
.globl insn_528
insn_528:
.option push
.option rvc
    c.addi4spn s0, sp, 4
.option pop
.globl insn_529
insn_529:
.option push
.option rvc
    c.addi4spn a5, sp, 1020
.option pop
.globl insn_530
insn_530:
.option push
.option rvc
    c.lw a0, 0(a1)
.option pop
.globl insn_531
insn_531:
.option push
.option rvc
    c.sw a0, 0(a1)
.option pop
.globl insn_532
insn_532:
.option push
.option rvc
    c.lw a0, 4(a1)
.option pop
.globl insn_533
insn_533:
.option push
.option rvc
    c.sw a0, 4(a1)
.option pop
.globl insn_534
insn_534:
.option push
.option rvc
    c.lw a0, 8(a1)
.option pop
.globl insn_535
insn_535:
.option push
.option rvc
    c.sw a0, 8(a1)
.option pop
.globl insn_536
insn_536:
.option push
.option rvc
    c.lw a0, 16(a1)
.option pop
.globl insn_537
insn_537:
.option push
.option rvc
    c.sw a0, 16(a1)
.option pop
.globl insn_538
insn_538:
.option push
.option rvc
    c.lw a0, 32(a1)
.option pop
.globl insn_539
insn_539:
.option push
.option rvc
    c.sw a0, 32(a1)
.option pop
.globl insn_540
insn_540:
.option push
.option rvc
    c.lw a0, 64(a1)
.option pop
.globl insn_541
insn_541:
.option push
.option rvc
    c.sw a0, 64(a1)
.option pop
.globl insn_542
insn_542:
.option push
.option rvc
    c.lw a0, 124(a1)
.option pop
.globl insn_543
insn_543:
.option push
.option rvc
    c.sw a0, 124(a1)
.option pop
.globl insn_544
insn_544:
.option push
.option rvc
    c.lw a0, 84(a1)
.option pop
.globl insn_545
insn_545:
.option push
.option rvc
    c.sw a0, 84(a1)
.option pop
.globl insn_546
insn_546:
.option push
.option rvc
    c.lw a0, 40(a1)
.option pop
.globl insn_547
insn_547:
.option push
.option rvc
    c.sw a0, 40(a1)
.option pop
.globl insn_548
insn_548:
.option push
.option rvc
    c.lw s0, 4(a5)
.option pop
.globl insn_549
insn_549:
.option push
.option rvc
    c.lw a5, 4(s0)
.option pop
.globl insn_550
insn_550:
.option push
.option rvc
    c.lw a0, 0(a0)
.option pop
.globl insn_551
insn_551:
.option push
.option rvc
    c.sw s0, 4(a5)
.option pop
.globl insn_552
insn_552:
.option push
.option rvc
    c.sw a5, 4(s0)
.option pop
.globl insn_553
insn_553:
.option push
.option rvc
    c.sw a0, 0(a0)
.option pop
.globl insn_554
insn_554:
.option push
.option rvc
    c.nop
.option pop
.globl insn_555
insn_555:
.option push
.option rvc
    c.addi a0, 1
.option pop
.globl insn_556
insn_556:
.option push
.option rvc
    c.li a0, 1
.option pop
.globl insn_557
insn_557:
.option push
.option rvc
    c.andi a0, 1
.option pop
.globl insn_558
insn_558:
.option push
.option rvc
    c.lui a0, 0x1
.option pop
.globl insn_559
insn_559:
.option push
.option rvc
    c.addi a0, 2
.option pop
.globl insn_560
insn_560:
.option push
.option rvc
    c.li a0, 2
.option pop
.globl insn_561
insn_561:
.option push
.option rvc
    c.andi a0, 2
.option pop
.globl insn_562
insn_562:
.option push
.option rvc
    c.lui a0, 0x2
.option pop
.globl insn_563
insn_563:
.option push
.option rvc
    c.addi a0, 4
.option pop
.globl insn_564
insn_564:
.option push
.option rvc
    c.li a0, 4
.option pop
.globl insn_565
insn_565:
.option push
.option rvc
    c.andi a0, 4
.option pop
.globl insn_566
insn_566:
.option push
.option rvc
    c.lui a0, 0x4
.option pop
.globl insn_567
insn_567:
.option push
.option rvc
    c.addi a0, 8
.option pop
.globl insn_568
insn_568:
.option push
.option rvc
    c.li a0, 8
.option pop
.globl insn_569
insn_569:
.option push
.option rvc
    c.andi a0, 8
.option pop
.globl insn_570
insn_570:
.option push
.option rvc
    c.lui a0, 0x8
.option pop
.globl insn_571
insn_571:
.option push
.option rvc
    c.addi a0, 16
.option pop
.globl insn_572
insn_572:
.option push
.option rvc
    c.li a0, 16
.option pop
.globl insn_573
insn_573:
.option push
.option rvc
    c.andi a0, 16
.option pop
.globl insn_574
insn_574:
.option push
.option rvc
    c.lui a0, 0x10
.option pop
.globl insn_575
insn_575:
.option push
.option rvc
    c.addi a0, 31
.option pop
.globl insn_576
insn_576:
.option push
.option rvc
    c.li a0, 31
.option pop
.globl insn_577
insn_577:
.option push
.option rvc
    c.andi a0, 31
.option pop
.globl insn_578
insn_578:
.option push
.option rvc
    c.lui a0, 0x1f
.option pop
.globl insn_579
insn_579:
.option push
.option rvc
    c.addi a0, -1
.option pop
.globl insn_580
insn_580:
.option push
.option rvc
    c.li a0, -1
.option pop
.globl insn_581
insn_581:
.option push
.option rvc
    c.andi a0, -1
.option pop
.globl insn_582
insn_582:
.option push
.option rvc
    c.lui a0, 0xfffff
.option pop
.globl insn_583
insn_583:
.option push
.option rvc
    c.addi a0, -32
.option pop
.globl insn_584
insn_584:
.option push
.option rvc
    c.li a0, -32
.option pop
.globl insn_585
insn_585:
.option push
.option rvc
    c.andi a0, -32
.option pop
.globl insn_586
insn_586:
.option push
.option rvc
    c.lui a0, 0xfffe0
.option pop
.globl insn_587
insn_587:
.option push
.option rvc
    c.addi a0, 21
.option pop
.globl insn_588
insn_588:
.option push
.option rvc
    c.li a0, 21
.option pop
.globl insn_589
insn_589:
.option push
.option rvc
    c.andi a0, 21
.option pop
.globl insn_590
insn_590:
.option push
.option rvc
    c.lui a0, 0x15
.option pop
.globl insn_591
insn_591:
.option push
.option rvc
    c.addi a0, -22
.option pop
.globl insn_592
insn_592:
.option push
.option rvc
    c.li a0, -22
.option pop
.globl insn_593
insn_593:
.option push
.option rvc
    c.andi a0, -22
.option pop
.globl insn_594
insn_594:
.option push
.option rvc
    c.lui a0, 0xfffea
.option pop
.globl insn_595
insn_595:
.option push
.option rvc
    c.addi ra, 1
.option pop
.globl insn_596
insn_596:
.option push
.option rvc
    c.addi t6, -1
.option pop
.globl insn_597
insn_597:
.option push
.option rvc
    c.addi sp, 4
.option pop
.globl insn_598
insn_598:
.option push
.option rvc
    c.li a0, 0
.option pop
.globl insn_599
insn_599:
.option push
.option rvc
    c.li ra, 5
.option pop
.globl insn_600
insn_600:
.option push
.option rvc
    c.li t6, -5
.option pop
.globl insn_601
insn_601:
.option push
.option rvc
    c.andi a0, 0
.option pop
.globl insn_602
insn_602:
.option push
.option rvc
    c.andi s0, 15
.option pop
.globl insn_603
insn_603:
.option push
.option rvc
    c.andi a5, -16
.option pop
.globl insn_604
insn_604:
.option push
.option rvc
    c.lui ra, 1
.option pop
.globl insn_605
insn_605:
.option push
.option rvc
    c.lui t6, 0xfffff
.option pop
.globl insn_606
insn_606:
.option push
.option rvc
    c.lui gp, 0xfffe0
.option pop
.globl insn_607
insn_607:
.option push
.option rvc
    c.addi16sp sp, 16
.option pop
.globl insn_608
insn_608:
.option push
.option rvc
    c.addi16sp sp, 32
.option pop
.globl insn_609
insn_609:
.option push
.option rvc
    c.addi16sp sp, 64
.option pop
.globl insn_610
insn_610:
.option push
.option rvc
    c.addi16sp sp, 128
.option pop
.globl insn_611
insn_611:
.option push
.option rvc
    c.addi16sp sp, 256
.option pop
.globl insn_612
insn_612:
.option push
.option rvc
    c.addi16sp sp, 496
.option pop
.globl insn_613
insn_613:
.option push
.option rvc
    c.addi16sp sp, -16
.option pop
.globl insn_614
insn_614:
.option push
.option rvc
    c.addi16sp sp, -512
.option pop
.globl insn_615
insn_615:
.option push
.option rvc
    c.addi16sp sp, 336
.option pop
.globl insn_616
insn_616:
.option push
.option rvc
    c.addi16sp sp, -352
.option pop
.globl insn_617
insn_617:
.option push
.option rvc
    c.srli a0, 1
.option pop
.globl insn_618
insn_618:
.option push
.option rvc
    c.srai a0, 1
.option pop
.globl insn_619
insn_619:
.option push
.option rvc
    c.slli a0, 1
.option pop
.globl insn_620
insn_620:
.option push
.option rvc
    c.srli a0, 2
.option pop
.globl insn_621
insn_621:
.option push
.option rvc
    c.srai a0, 2
.option pop
.globl insn_622
insn_622:
.option push
.option rvc
    c.slli a0, 2
.option pop
.globl insn_623
insn_623:
.option push
.option rvc
    c.srli a0, 4
.option pop
.globl insn_624
insn_624:
.option push
.option rvc
    c.srai a0, 4
.option pop
.globl insn_625
insn_625:
.option push
.option rvc
    c.slli a0, 4
.option pop
.globl insn_626
insn_626:
.option push
.option rvc
    c.srli a0, 8
.option pop
.globl insn_627
insn_627:
.option push
.option rvc
    c.srai a0, 8
.option pop
.globl insn_628
insn_628:
.option push
.option rvc
    c.slli a0, 8
.option pop
.globl insn_629
insn_629:
.option push
.option rvc
    c.srli a0, 16
.option pop
.globl insn_630
insn_630:
.option push
.option rvc
    c.srai a0, 16
.option pop
.globl insn_631
insn_631:
.option push
.option rvc
    c.slli a0, 16
.option pop
.globl insn_632
insn_632:
.option push
.option rvc
    c.srli a0, 21
.option pop
.globl insn_633
insn_633:
.option push
.option rvc
    c.srai a0, 21
.option pop
.globl insn_634
insn_634:
.option push
.option rvc
    c.slli a0, 21
.option pop
.globl insn_635
insn_635:
.option push
.option rvc
    c.srli a0, 31
.option pop
.globl insn_636
insn_636:
.option push
.option rvc
    c.srai a0, 31
.option pop
.globl insn_637
insn_637:
.option push
.option rvc
    c.slli a0, 31
.option pop
.globl insn_638
insn_638:
.option push
.option rvc
    c.srli s0, 4
.option pop
.globl insn_639
insn_639:
.option push
.option rvc
    c.srli a5, 4
.option pop
.globl insn_640
insn_640:
.option push
.option rvc
    c.srai s0, 4
.option pop
.globl insn_641
insn_641:
.option push
.option rvc
    c.srai a5, 4
.option pop
.globl insn_642
insn_642:
.option push
.option rvc
    c.slli ra, 4
.option pop
.globl insn_643
insn_643:
.option push
.option rvc
    c.slli t6, 4
.option pop
.globl insn_644
insn_644:
.option push
.option rvc
    c.sub a0, a1
.option pop
.globl insn_645
insn_645:
.option push
.option rvc
    c.sub s0, a5
.option pop
.globl insn_646
insn_646:
.option push
.option rvc
    c.sub a5, s0
.option pop
.globl insn_647
insn_647:
.option push
.option rvc
    c.sub s1, s1
.option pop
.globl insn_648
insn_648:
.option push
.option rvc
    c.xor a0, a1
.option pop
.globl insn_649
insn_649:
.option push
.option rvc
    c.xor s0, a5
.option pop
.globl insn_650
insn_650:
.option push
.option rvc
    c.xor a5, s0
.option pop
.globl insn_651
insn_651:
.option push
.option rvc
    c.xor s1, s1
.option pop
.globl insn_652
insn_652:
.option push
.option rvc
    c.or a0, a1
.option pop
.globl insn_653
insn_653:
.option push
.option rvc
    c.or s0, a5
.option pop
.globl insn_654
insn_654:
.option push
.option rvc
    c.or a5, s0
.option pop
.globl insn_655
insn_655:
.option push
.option rvc
    c.or s1, s1
.option pop
.globl insn_656
insn_656:
.option push
.option rvc
    c.and a0, a1
.option pop
.globl insn_657
insn_657:
.option push
.option rvc
    c.and s0, a5
.option pop
.globl insn_658
insn_658:
.option push
.option rvc
    c.and a5, s0
.option pop
.globl insn_659
insn_659:
.option push
.option rvc
    c.and s1, s1
.option pop
.globl insn_660
insn_660:
.option push
.option rvc
    c.j -2
.option pop
.globl insn_661
insn_661:
.option push
.option rvc
    c.jal -2
.option pop
.globl insn_662
insn_662:
.option push
.option rvc
    c.j 2046
.option pop
.globl insn_663
insn_663:
.option push
.option rvc
    c.jal 2046
.option pop
.globl insn_664
insn_664:
.option push
.option rvc
    c.j -2048
.option pop
.globl insn_665
insn_665:
.option push
.option rvc
    c.jal -2048
.option pop
.globl insn_666
insn_666:
.option push
.option rvc
    c.j 1364
.option pop
.globl insn_667
insn_667:
.option push
.option rvc
    c.jal 1364
.option pop
.globl insn_668
insn_668:
.option push
.option rvc
    c.j -1366
.option pop
.globl insn_669
insn_669:
.option push
.option rvc
    c.jal -1366
.option pop
.globl insn_670
insn_670:
.option push
.option rvc
    c.j 682
.option pop
.globl insn_671
insn_671:
.option push
.option rvc
    c.jal 682
.option pop
.globl insn_672
insn_672:
.option push
.option rvc
    c.j 2
.option pop
.globl insn_673
insn_673:
.option push
.option rvc
    c.jal 2
.option pop
.globl insn_674
insn_674:
.option push
.option rvc
    c.j 4
.option pop
.globl insn_675
insn_675:
.option push
.option rvc
    c.jal 4
.option pop
.globl insn_676
insn_676:
.option push
.option rvc
    c.j 8
.option pop
.globl insn_677
insn_677:
.option push
.option rvc
    c.jal 8
.option pop
.globl insn_678
insn_678:
.option push
.option rvc
    c.j 16
.option pop
.globl insn_679
insn_679:
.option push
.option rvc
    c.jal 16
.option pop
.globl insn_680
insn_680:
.option push
.option rvc
    c.j 32
.option pop
.globl insn_681
insn_681:
.option push
.option rvc
    c.jal 32
.option pop
.globl insn_682
insn_682:
.option push
.option rvc
    c.j 64
.option pop
.globl insn_683
insn_683:
.option push
.option rvc
    c.jal 64
.option pop
.globl insn_684
insn_684:
.option push
.option rvc
    c.j 128
.option pop
.globl insn_685
insn_685:
.option push
.option rvc
    c.jal 128
.option pop
.globl insn_686
insn_686:
.option push
.option rvc
    c.j 256
.option pop
.globl insn_687
insn_687:
.option push
.option rvc
    c.jal 256
.option pop
.globl insn_688
insn_688:
.option push
.option rvc
    c.j 512
.option pop
.globl insn_689
insn_689:
.option push
.option rvc
    c.jal 512
.option pop
.globl insn_690
insn_690:
.option push
.option rvc
    c.j 1024
.option pop
.globl insn_691
insn_691:
.option push
.option rvc
    c.jal 1024
.option pop
.globl insn_692
insn_692:
.option push
.option rvc
    c.beqz a0, -2
.option pop
.globl insn_693
insn_693:
.option push
.option rvc
    c.bnez a0, -2
.option pop
.globl insn_694
insn_694:
.option push
.option rvc
    c.beqz a0, 254
.option pop
.globl insn_695
insn_695:
.option push
.option rvc
    c.bnez a0, 254
.option pop
.globl insn_696
insn_696:
.option push
.option rvc
    c.beqz a0, -256
.option pop
.globl insn_697
insn_697:
.option push
.option rvc
    c.bnez a0, -256
.option pop
.globl insn_698
insn_698:
.option push
.option rvc
    c.beqz a0, 84
.option pop
.globl insn_699
insn_699:
.option push
.option rvc
    c.bnez a0, 84
.option pop
.globl insn_700
insn_700:
.option push
.option rvc
    c.beqz a0, -86
.option pop
.globl insn_701
insn_701:
.option push
.option rvc
    c.bnez a0, -86
.option pop
.globl insn_702
insn_702:
.option push
.option rvc
    c.beqz a0, 170
.option pop
.globl insn_703
insn_703:
.option push
.option rvc
    c.bnez a0, 170
.option pop
.globl insn_704
insn_704:
.option push
.option rvc
    c.beqz a0, 2
.option pop
.globl insn_705
insn_705:
.option push
.option rvc
    c.bnez a0, 2
.option pop
.globl insn_706
insn_706:
.option push
.option rvc
    c.beqz a0, 4
.option pop
.globl insn_707
insn_707:
.option push
.option rvc
    c.bnez a0, 4
.option pop
.globl insn_708
insn_708:
.option push
.option rvc
    c.beqz a0, 8
.option pop
.globl insn_709
insn_709:
.option push
.option rvc
    c.bnez a0, 8
.option pop
.globl insn_710
insn_710:
.option push
.option rvc
    c.beqz a0, 16
.option pop
.globl insn_711
insn_711:
.option push
.option rvc
    c.bnez a0, 16
.option pop
.globl insn_712
insn_712:
.option push
.option rvc
    c.beqz a0, 32
.option pop
.globl insn_713
insn_713:
.option push
.option rvc
    c.bnez a0, 32
.option pop
.globl insn_714
insn_714:
.option push
.option rvc
    c.beqz a0, 64
.option pop
.globl insn_715
insn_715:
.option push
.option rvc
    c.bnez a0, 64
.option pop
.globl insn_716
insn_716:
.option push
.option rvc
    c.beqz a0, 128
.option pop
.globl insn_717
insn_717:
.option push
.option rvc
    c.bnez a0, 128
.option pop
.globl insn_718
insn_718:
.option push
.option rvc
    c.beqz s0, 8
.option pop
.globl insn_719
insn_719:
.option push
.option rvc
    c.beqz a5, 8
.option pop
.globl insn_720
insn_720:
.option push
.option rvc
    c.bnez s0, 8
.option pop
.globl insn_721
insn_721:
.option push
.option rvc
    c.bnez a5, 8
.option pop
.globl insn_722
insn_722:
.option push
.option rvc
    c.lwsp a0, 0(sp)
.option pop
.globl insn_723
insn_723:
.option push
.option rvc
    c.swsp a0, 0(sp)
.option pop
.globl insn_724
insn_724:
.option push
.option rvc
    c.lwsp a0, 4(sp)
.option pop
.globl insn_725
insn_725:
.option push
.option rvc
    c.swsp a0, 4(sp)
.option pop
.globl insn_726
insn_726:
.option push
.option rvc
    c.lwsp a0, 8(sp)
.option pop
.globl insn_727
insn_727:
.option push
.option rvc
    c.swsp a0, 8(sp)
.option pop
.globl insn_728
insn_728:
.option push
.option rvc
    c.lwsp a0, 16(sp)
.option pop
.globl insn_729
insn_729:
.option push
.option rvc
    c.swsp a0, 16(sp)
.option pop
.globl insn_730
insn_730:
.option push
.option rvc
    c.lwsp a0, 32(sp)
.option pop
.globl insn_731
insn_731:
.option push
.option rvc
    c.swsp a0, 32(sp)
.option pop
.globl insn_732
insn_732:
.option push
.option rvc
    c.lwsp a0, 64(sp)
.option pop
.globl insn_733
insn_733:
.option push
.option rvc
    c.swsp a0, 64(sp)
.option pop
.globl insn_734
insn_734:
.option push
.option rvc
    c.lwsp a0, 128(sp)
.option pop
.globl insn_735
insn_735:
.option push
.option rvc
    c.swsp a0, 128(sp)
.option pop
.globl insn_736
insn_736:
.option push
.option rvc
    c.lwsp a0, 252(sp)
.option pop
.globl insn_737
insn_737:
.option push
.option rvc
    c.swsp a0, 252(sp)
.option pop
.globl insn_738
insn_738:
.option push
.option rvc
    c.lwsp a0, 84(sp)
.option pop
.globl insn_739
insn_739:
.option push
.option rvc
    c.swsp a0, 84(sp)
.option pop
.globl insn_740
insn_740:
.option push
.option rvc
    c.lwsp a0, 168(sp)
.option pop
.globl insn_741
insn_741:
.option push
.option rvc
    c.swsp a0, 168(sp)
.option pop
.globl insn_742
insn_742:
.option push
.option rvc
    c.lwsp ra, 4(sp)
.option pop
.globl insn_743
insn_743:
.option push
.option rvc
    c.lwsp t6, 4(sp)
.option pop
.globl insn_744
insn_744:
.option push
.option rvc
    c.lwsp sp, 4(sp)
.option pop
.globl insn_745
insn_745:
.option push
.option rvc
    c.swsp ra, 4(sp)
.option pop
.globl insn_746
insn_746:
.option push
.option rvc
    c.swsp t6, 4(sp)
.option pop
.globl insn_747
insn_747:
.option push
.option rvc
    c.swsp zero, 4(sp)
.option pop
.globl insn_748
insn_748:
.option push
.option rvc
    c.swsp sp, 4(sp)
.option pop
.globl insn_749
insn_749:
.option push
.option rvc
    c.jr a0
.option pop
.globl insn_750
insn_750:
.option push
.option rvc
    c.jr ra
.option pop
.globl insn_751
insn_751:
.option push
.option rvc
    c.jr t6
.option pop
.globl insn_752
insn_752:
.option push
.option rvc
    c.jalr a0
.option pop
.globl insn_753
insn_753:
.option push
.option rvc
    c.jalr ra
.option pop
.globl insn_754
insn_754:
.option push
.option rvc
    c.jalr t6
.option pop
.globl insn_755
insn_755:
.option push
.option rvc
    c.mv a0, a1
.option pop
.globl insn_756
insn_756:
.option push
.option rvc
    c.mv t6, ra
.option pop
.globl insn_757
insn_757:
.option push
.option rvc
    c.mv ra, t6
.option pop
.globl insn_758
insn_758:
.option push
.option rvc
    c.mv sp, a0
.option pop
.globl insn_759
insn_759:
.option push
.option rvc
    c.add a0, a1
.option pop
.globl insn_760
insn_760:
.option push
.option rvc
    c.add t6, ra
.option pop
.globl insn_761
insn_761:
.option push
.option rvc
    c.add ra, t6
.option pop
.globl insn_762
insn_762:
.option push
.option rvc
    c.add a0, a0
.option pop
.globl insn_763
insn_763:
.option push
.option rvc
    c.add sp, a0
.option pop
.globl insn_764
insn_764:
.option push
.option norvc
    ecall
.option pop
.globl insn_765
insn_765:
.option push
.option norvc
    jal zero, 0
.option pop
.globl insn_766
insn_766:
.option push
.option norvc
    addi a7, zero, 0
    addi a0, zero, 42
    ecall
.option pop
.globl insn_767
insn_767:
.option push
.option norvc
    1:
    addi a0, a0, 1
    bne a2, zero, 2f
    addi a2, zero, 1
    sw a1, 0(t0)
    jal zero, 1b
    2:
.option pop
.globl insn_768
insn_768:
.option push
.option norvc
    addi a0, a0, 100
.option pop
.globl insn_769
insn_769:
.option push
.option norvc
    addi a0, a0, 1
.option pop
.globl insn_770
insn_770:
.option push
.option norvc
    1:
    addi a0, a0, 1
    blt a0, a1, 1b
.option pop
.globl insn_771
insn_771:
